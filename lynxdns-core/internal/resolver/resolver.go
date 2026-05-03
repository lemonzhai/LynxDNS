package resolver

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
	xcache "github.com/lynxdns/lynxdns-core/internal/cache"
	xclient "github.com/lynxdns/lynxdns-core/internal/client"
	"github.com/lynxdns/lynxdns-core/internal/config"
	xlog "github.com/lynxdns/lynxdns-core/internal/log"
	"github.com/lynxdns/lynxdns-core/internal/rules"
	"github.com/lynxdns/lynxdns-core/internal/geodata"
)

type Action string

const (
	ActionCacheHit  Action = "cache_hit"
	ActionDomestic  Action = "domestic"
	ActionRemote    Action = "remote"
	ActionBlocked   Action = "blocked"
	ActionRedirect  Action = "redirected"
	ActionDefault   Action = "default"
)

type QueryLog struct {
	Timestamp   time.Time `json:"timestamp"`
	Domain      string    `json:"domain"`
	Type        string    `json:"type"`
	ClientIP    string    `json:"client_ip"`
	ServerUsed  string    `json:"server_used"`
	ResultIPs   []string  `json:"ips"`
	LatencyMs   float64   `json:"latency_ms"`
	Action      Action    `json:"action"`
	MatchedRule string    `json:"matched_rule"`
	Cached      bool      `json:"cached"`
	TTL         uint32    `json:"ttl"`
}

type Stats struct {
	TotalQueries      int64            `json:"total_queries"`
	CacheHits         int64            `json:"cache_hits"`
	CacheHitRate      float64          `json:"cache_hit_rate"`
	AvgLatencyMs      float64          `json:"avg_latency_ms"`
	QueriesPerSecond  float64          `json:"queries_per_second"`
	BlockedQueries    int64            `json:"blocked_queries"`
	RedirectedQueries int64            `json:"redirected_queries"`
	ByType            map[string]int64 `json:"by_type"`
	ByStatus          map[string]int64 `json:"by_status"`
	ByAction          map[string]int64 `json:"by_action"`
}

var localDomainSuffixes = []string{
	".lan",
	".local",
	".localhost",
	".home",
	".internal",
	".test",
	".example",
	".invalid",
	".onion",
	".arpa",
}

type Resolver struct {
	mu          sync.RWMutex
	cfg         *config.FullConfig
	client      *xclient.DNSClient
	cache       *xcache.DNSCache
	ruleEngine  *rules.Engine
	geoManager  *geodata.Manager

	totalQueries     int64
	cacheHits        int64
	blockedQueries   int64
	redirectedQueries int64
	totalLatencyNs   int64
	byType           sync.Map
	byStatus         sync.Map
	byAction         sync.Map

	subscribers []chan QueryLog
	subMu       sync.Mutex
	startTime   time.Time

	queryHistory []*QueryLog
	histMu       sync.Mutex
}

func New(cfg *config.FullConfig, client *xclient.DNSClient, cache *xcache.DNSCache, ruleEngine *rules.Engine, geoMgr *geodata.Manager) *Resolver {
	r := &Resolver{
		cfg:        cfg,
		client:     client,
		cache:      cache,
		ruleEngine: ruleEngine,
		geoManager: geoMgr,
		startTime:  time.Now(),
	}

	r.initUpstreams()
	return r
}

func (r *Resolver) UpdateConfig(cfg *config.FullConfig) {
	r.mu.Lock()
	r.cfg = cfg
	r.mu.Unlock()
	r.initUpstreams()
}

func (r *Resolver) initUpstreams() {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	addUpstream := func(addr string) {
		if !seen[addr] {
			seen[addr] = true
			r.client.AddUpstream(addr)
		}
	}

	for _, addr := range r.cfg.DNS.Domestic {
		addUpstream(addr)
	}
	for _, addr := range r.cfg.DNS.Remote {
		addUpstream(addr)
	}
	for _, addr := range r.cfg.DNS.Bootstrap {
		addUpstream(addr)
	}
}

func (r *Resolver) Resolve(msg *dns.Msg, clientAddr string) *dns.Msg {
	if len(msg.Question) == 0 {
		return new(dns.Msg).SetRcode(msg, dns.RcodeFormatError)
	}

	q := msg.Question[0]
	domain := strings.TrimSuffix(q.Name, ".")
	qtypeStr := dns.TypeToString[q.Qtype]

	atomic.AddInt64(&r.totalQueries, 1)
	r.incrementMap(&r.byType, qtypeStr)

	start := time.Now()

	if cached, ok := r.cache.Get(domain, q.Qtype); ok {
		atomic.AddInt64(&r.cacheHits, 1)
		r.incrementMap(&r.byAction, string(ActionCacheHit))
		r.incrementMap(&r.byStatus, "success")

		r.emitLog(QueryLog{
			Timestamp:   time.Now().UTC(),
			Domain:      domain,
			Type:        qtypeStr,
			ClientIP:    clientAddr,
			ServerUsed:  "cache",
			LatencyMs:   0,
			Action:      ActionCacheHit,
			Cached:      true,
		})

		resp := cached.Copy()
		resp.Id = msg.Id
		return resp
	}

	result := r.resolveWithRules(msg, domain, q, clientAddr)

	latency := time.Since(start)
	atomic.AddInt64(&r.totalLatencyNs, latency.Nanoseconds())

	if result != nil && result.Rcode == dns.RcodeSuccess {
		r.incrementMap(&r.byStatus, "success")
	} else {
		r.incrementMap(&r.byStatus, "failed")
	}

	return result
}

func (r *Resolver) resolveWithRules(msg *dns.Msg, domain string, q dns.Question, clientAddr string) *dns.Msg {
	r.mu.RLock()
	cfg := r.cfg
	r.mu.RUnlock()

	ruleResult := r.ruleEngine.Match(domain)
	if ruleResult.Matched {
		switch ruleResult.RuleType {
		case rules.TypeBlock:
			atomic.AddInt64(&r.blockedQueries, 1)
			r.incrementMap(&r.byAction, string(ActionBlocked))
			r.emitLog(QueryLog{
				Timestamp:   time.Now().UTC(),
				Domain:      domain,
				Type:        dns.TypeToString[q.Qtype],
				ClientIP:    clientAddr,
				Action:      ActionBlocked,
				MatchedRule: ruleResult.RuleID + ":" + ruleResult.RuleDomain,
			})
			return r.blockResponse(msg)

		case rules.TypeRedirect:
			atomic.AddInt64(&r.redirectedQueries, 1)
			r.incrementMap(&r.byAction, string(ActionRedirect))
			r.emitLog(QueryLog{
				Timestamp:   time.Now().UTC(),
				Domain:      domain,
				Type:        dns.TypeToString[q.Qtype],
				ClientIP:    clientAddr,
				ResultIPs:   []string{ruleResult.Target},
				Action:      ActionRedirect,
				MatchedRule: ruleResult.RuleID + ":" + ruleResult.RuleDomain,
			})
			return r.redirectResponse(msg, q, ruleResult.Target)

		case rules.TypeRemote:
			return r.forwardToGroup(msg, domain, q, clientAddr, cfg.DNS.Remote, ActionRemote, "rule:"+ruleResult.RuleID)

		case rules.TypeDomestic:
			return r.forwardToGroup(msg, domain, q, clientAddr, cfg.DNS.Domestic, ActionDomestic, "rule:"+ruleResult.RuleID)
		}
	}

	if cfg.Advanced.AdFilter.Enabled && r.geoManager.MatchAdFilter(domain) {
		atomic.AddInt64(&r.blockedQueries, 1)
		r.incrementMap(&r.byAction, string(ActionBlocked))
		r.emitLog(QueryLog{
			Timestamp:   time.Now().UTC(),
			Domain:      domain,
			Type:        dns.TypeToString[q.Qtype],
			ClientIP:    clientAddr,
			Action:      ActionBlocked,
			MatchedRule: "ad_filter",
		})
		return r.blockResponse(msg)
	}

	remoteCategories := cfg.Routing.Geosite.Remote
	if len(remoteCategories) > 0 && r.geoManager.MatchGeosite(domain, remoteCategories) {
		return r.forwardToGroup(msg, domain, q, clientAddr, cfg.DNS.Remote, ActionRemote, "geosite:matched")
	}

	domesticCategories := cfg.Routing.Geosite.Domestic
	if len(domesticCategories) > 0 && r.geoManager.MatchGeosite(domain, domesticCategories) {
		return r.forwardToGroup(msg, domain, q, clientAddr, cfg.DNS.Domestic, ActionDomestic, "geosite:matched")
	}

	adFilterCategories := cfg.Routing.Geosite.AdFilter
	if cfg.Advanced.AdFilter.Enabled && len(adFilterCategories) > 0 && r.geoManager.MatchGeosite(domain, adFilterCategories) {
		atomic.AddInt64(&r.blockedQueries, 1)
		r.incrementMap(&r.byAction, string(ActionBlocked))
		r.emitLog(QueryLog{
			Timestamp:   time.Now().UTC(),
			Domain:      domain,
			Type:        dns.TypeToString[q.Qtype],
			ClientIP:    clientAddr,
			Action:      ActionBlocked,
			MatchedRule: "geosite:ad_filter",
		})
		return r.blockResponse(msg)
	}

	if cfg.Advanced.LeakProtection.Enabled && !isLocalDomain(domain) {
		hasRemote := len(remoteCategories) > 0 && r.geoManager.HasGeositeCategories(remoteCategories)
		hasDomestic := len(domesticCategories) > 0 && r.geoManager.HasGeositeCategories(domesticCategories)

		switch cfg.Advanced.LeakProtection.Mode {
		case "strict":
			if hasRemote && hasDomestic {
				atomic.AddInt64(&r.blockedQueries, 1)
				r.incrementMap(&r.byAction, string(ActionBlocked))
				r.emitLog(QueryLog{
					Timestamp:   time.Now().UTC(),
					Domain:      domain,
					Type:        dns.TypeToString[q.Qtype],
					ClientIP:    clientAddr,
					Action:      ActionBlocked,
					MatchedRule: "leak_protection:strict",
				})
				return new(dns.Msg).SetRcode(msg, dns.RcodeNameError)
			}
		case "standard":
			if hasRemote && hasDomestic {
				return r.forwardToGroup(msg, domain, q, clientAddr, cfg.DNS.Remote, ActionDefault, "leak_protection:standard")
			}
		}
	}

	return r.forwardToDefault(msg, domain, q, clientAddr)
}

func (r *Resolver) forwardToGroup(msg *dns.Msg, domain string, q dns.Question, clientAddr string, servers []string, action Action, matchedRule string) *dns.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := r.client.QueryGroup(ctx, msg, servers, r.getConfig().Advanced.Concurrency)

	if result.Error != nil {
		xlog.Warn("DNS query failed for %s via %v: %v", domain, servers, result.Error)
		r.incrementMap(&r.byStatus, "timeout")
		return new(dns.Msg).SetRcode(msg, dns.RcodeServerFailure)
	}

	r.cache.Set(domain, q.Qtype, result.Msg, result.ServerUsed)
	r.incrementMap(&r.byAction, string(action))

	ips := extractIPsFromMsg(result.Msg)
	r.emitLog(QueryLog{
		Timestamp:   time.Now().UTC(),
		Domain:      domain,
		Type:        dns.TypeToString[q.Qtype],
		ClientIP:    clientAddr,
		ServerUsed:  result.ServerUsed,
		ResultIPs:   ips,
		LatencyMs:   float64(result.Latency.Microseconds()) / 1000.0,
		Action:      action,
		MatchedRule: matchedRule,
	})

	return result.Msg
}

func (r *Resolver) forwardToDefault(msg *dns.Msg, domain string, q dns.Question, clientAddr string) *dns.Msg {
	r.mu.RLock()
	cfg := r.cfg
	r.mu.RUnlock()

	var servers []string
	switch cfg.DNS.Default {
	case "remote":
		servers = cfg.DNS.Remote
	default:
		servers = cfg.DNS.Domestic
	}

	return r.forwardToGroup(msg, domain, q, clientAddr, servers, ActionDefault, "default_policy")
}

func (r *Resolver) Lookup(ctx context.Context, domain string, qtype uint16) (*QueryLog, error) {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(domain), qtype)
	msg.RecursionDesired = true

	ql := &QueryLog{
		Timestamp: time.Now().UTC(),
		Domain:    domain,
		Type:      dns.TypeToString[qtype],
	}

	result := r.ruleEngine.Match(domain)
	if result.Matched {
		switch result.RuleType {
		case rules.TypeBlock:
			ql.Action = ActionBlocked
			ql.MatchedRule = result.RuleID
			return ql, nil
		case rules.TypeRedirect:
			ql.Action = ActionRedirect
			ql.ResultIPs = []string{result.Target}
			ql.MatchedRule = result.RuleID
			return ql, nil
		case rules.TypeRemote:
			r.mu.RLock()
			servers := r.cfg.DNS.Remote
			r.mu.RUnlock()
			return r.doLookup(ctx, msg, domain, qtype, servers, ActionRemote, ql)
		case rules.TypeDomestic:
			r.mu.RLock()
			servers := r.cfg.DNS.Domestic
			r.mu.RUnlock()
			return r.doLookup(ctx, msg, domain, qtype, servers, ActionDomestic, ql)
		}
	}

	r.mu.RLock()
	cfg := r.cfg
	r.mu.RUnlock()

	if cfg.Advanced.AdFilter.Enabled && r.geoManager.MatchAdFilter(domain) {
		ql.Action = ActionBlocked
		ql.MatchedRule = "ad_filter"
		return ql, nil
	}

	remoteCategories := cfg.Routing.Geosite.Remote
	if len(remoteCategories) > 0 && r.geoManager.MatchGeosite(domain, remoteCategories) {
		return r.doLookup(ctx, msg, domain, qtype, cfg.DNS.Remote, ActionRemote, ql)
	}

	domesticCategories := cfg.Routing.Geosite.Domestic
	if len(domesticCategories) > 0 && r.geoManager.MatchGeosite(domain, domesticCategories) {
		return r.doLookup(ctx, msg, domain, qtype, cfg.DNS.Domestic, ActionDomestic, ql)
	}

	adFilterCategories := cfg.Routing.Geosite.AdFilter
	if cfg.Advanced.AdFilter.Enabled && len(adFilterCategories) > 0 && r.geoManager.MatchGeosite(domain, adFilterCategories) {
		ql.Action = ActionBlocked
		ql.MatchedRule = "geosite:ad_filter"
		return ql, nil
	}

	if cfg.Advanced.LeakProtection.Enabled && !isLocalDomain(domain) {
		hasRemote := len(remoteCategories) > 0 && r.geoManager.HasGeositeCategories(remoteCategories)
		hasDomestic := len(domesticCategories) > 0 && r.geoManager.HasGeositeCategories(domesticCategories)

		switch cfg.Advanced.LeakProtection.Mode {
		case "strict":
			if hasRemote && hasDomestic {
				ql.Action = ActionBlocked
				ql.MatchedRule = "leak_protection:strict"
				return ql, nil
			}
		case "standard":
			if hasRemote && hasDomestic {
				return r.doLookup(ctx, msg, domain, qtype, cfg.DNS.Remote, ActionDefault, ql)
			}
		}
	}

	var servers []string
	if cfg.DNS.Default == "remote" {
		servers = cfg.DNS.Remote
	} else {
		servers = cfg.DNS.Domestic
	}

	return r.doLookup(ctx, msg, domain, qtype, servers, ActionDefault, ql)
}

func (r *Resolver) doLookup(ctx context.Context, msg *dns.Msg, domain string, qtype uint16, servers []string, action Action, ql *QueryLog) (*QueryLog, error) {
	start := time.Now()
	result := r.client.QueryGroup(ctx, msg, servers, r.getConfig().Advanced.Concurrency)
	ql.LatencyMs = float64(time.Since(start).Microseconds()) / 1000.0

	if result.Error != nil {
		return ql, result.Error
	}

	ql.ServerUsed = result.ServerUsed
	ql.Action = action
	ql.ResultIPs = extractIPsFromMsg(result.Msg)
	ql.MatchedRule = string(action)
	if result.Msg != nil && len(result.Msg.Answer) > 0 {
		ql.TTL = result.Msg.Answer[0].Header().Ttl
	}

	return ql, nil
}

func (r *Resolver) GetStats() Stats {
	total := atomic.LoadInt64(&r.totalQueries)
	hits := atomic.LoadInt64(&r.cacheHits)
	blocked := atomic.LoadInt64(&r.blockedQueries)
	redirected := atomic.LoadInt64(&r.redirectedQueries)
	totalLatNs := atomic.LoadInt64(&r.totalLatencyNs)

	var avgLat float64
	if total > 0 {
		avgLat = float64(totalLatNs) / float64(total) / 1e6
	}

	var hitRate float64
	if total > 0 {
		hitRate = float64(hits) / float64(total) * 100
	}

	uptime := time.Since(r.startTime).Seconds()
	var qps float64
	if uptime > 0 {
		qps = float64(total) / uptime
	}

	return Stats{
		TotalQueries:      total,
		CacheHits:         hits,
		CacheHitRate:      hitRate,
		AvgLatencyMs:      avgLat,
		QueriesPerSecond:  qps,
		BlockedQueries:    blocked,
		RedirectedQueries: redirected,
		ByType:            r.syncMapToMap(&r.byType),
		ByStatus:          r.syncMapToMap(&r.byStatus),
		ByAction:          r.syncMapToMap(&r.byAction),
	}
}

func (r *Resolver) Subscribe() chan QueryLog {
	r.subMu.Lock()
	defer r.subMu.Unlock()
	ch := make(chan QueryLog, 256)
	r.subscribers = append(r.subscribers, ch)
	return ch
}

func (r *Resolver) Unsubscribe(ch chan QueryLog) {
	r.subMu.Lock()
	defer r.subMu.Unlock()
	for i, sub := range r.subscribers {
		if sub == ch {
			r.subscribers = append(r.subscribers[:i], r.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

func (r *Resolver) Uptime() time.Duration {
	return time.Since(r.startTime)
}

func (r *Resolver) emitLog(ql QueryLog) {
	// Get color based on action
	var color string
	switch ql.Action {
	case ActionDomestic:
		color = xlog.ColorDomestic  // 绿色 - 国内DNS
	case ActionRemote:
		color = xlog.ColorRemote    // 青色 - 远程DNS
	case ActionCacheHit:
		color = xlog.ColorCacheHit  // 黄色 - 缓存命中
	case ActionBlocked:
		color = xlog.ColorBlocked   // 红色 - 拦截
	case ActionRedirect:
		color = xlog.ColorRedirect  // 紫色 - 重定向
	case ActionDefault:
		color = xlog.ColorDefault   // 白色 - 默认策略
	default:
		color = xlog.ColorDefault
	}

	switch ql.Action {
	case ActionCacheHit:
		xlog.ColorInfo(color, "[DNS] %s %s %s -> cache hit (%.1fms)",
			ql.Action, ql.Type, ql.Domain, ql.LatencyMs)
	case ActionBlocked:
		xlog.ColorInfo(color, "[DNS] %s %s %s [%s]",
			ql.Action, ql.Type, ql.Domain, ql.MatchedRule)
	default:
		ips := strings.Join(ql.ResultIPs, ",")
		xlog.ColorInfo(color, "[DNS] %s %s %s -> %s [%s] (%.1fms via %s)",
			ql.Action, ql.Type, ql.Domain, ips, ql.MatchedRule, ql.LatencyMs, ql.ServerUsed)
	}

	r.histMu.Lock()
	r.queryHistory = append(r.queryHistory, &ql)
	if len(r.queryHistory) > 200 {
		r.queryHistory = r.queryHistory[len(r.queryHistory)-200:]
	}
	r.histMu.Unlock()

	r.subMu.Lock()
	defer r.subMu.Unlock()

	for _, sub := range r.subscribers {
		select {
		case sub <- ql:
		default:
		}
	}
}

func (r *Resolver) GetRecentQueries(limit int) []QueryLog {
	r.histMu.Lock()
	defer r.histMu.Unlock()
	n := len(r.queryHistory)
	if limit > 0 && limit < n {
		n = limit
	}
	result := make([]QueryLog, n)
	src := r.queryHistory[len(r.queryHistory)-n:]
	for i, p := range src {
		result[i] = *p
	}
	return result
}

func (r *Resolver) blockResponse(msg *dns.Msg) *dns.Msg {
	resp := new(dns.Msg)
	resp.SetRcode(msg, dns.RcodeSuccess)
	resp.Authoritative = true
	resp.RecursionAvailable = true
	return resp
}

func (r *Resolver) redirectResponse(msg *dns.Msg, q dns.Question, targetIP string) *dns.Msg {
	resp := new(dns.Msg)
	resp.SetReply(msg)
	resp.Authoritative = true
	resp.RecursionAvailable = true

	var rr dns.RR
	var err error
	switch q.Qtype {
	case dns.TypeAAAA:
		rr, err = dns.NewRR(fmt.Sprintf("%s 3600 IN AAAA %s", q.Name, targetIP))
	default:
		rr, err = dns.NewRR(fmt.Sprintf("%s 3600 IN A %s", q.Name, targetIP))
	}
	if err == nil {
		resp.Answer = append(resp.Answer, rr)
	}

	return resp
}

func isLocalDomain(domain string) bool {
	lower := strings.ToLower(domain)
	for _, suffix := range localDomainSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	parts := strings.Split(lower, ".")
	if len(parts) == 1 {
		return true
	}
	ld := parts[len(parts)-1]
	for _, tld := range localDomainSuffixes {
		if ld == tld[1:] {
			return true
		}
	}
	return false
}

func (r *Resolver) getConfig() *config.FullConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cfg
}

func (r *Resolver) incrementMap(m *sync.Map, key string) {
	for {
		val, loaded := m.LoadOrStore(key, int64(1))
		if !loaded {
			return
		}
		old := val.(int64)
		if m.CompareAndSwap(key, old, old+1) {
			return
		}
	}
}

func (r *Resolver) syncMapToMap(m *sync.Map) map[string]int64 {
	result := make(map[string]int64)
	m.Range(func(key, value interface{}) bool {
		result[key.(string)] = value.(int64)
		return true
	})
	return result
}

func extractIPsFromMsg(msg *dns.Msg) []string {
	var ips []string
	for _, rr := range msg.Answer {
		switch v := rr.(type) {
		case *dns.A:
			ips = append(ips, v.A.String())
		case *dns.AAAA:
			ips = append(ips, v.AAAA.String())
		case *dns.CNAME:
			ips = append(ips, v.Target)
		}
	}
	return ips
}
