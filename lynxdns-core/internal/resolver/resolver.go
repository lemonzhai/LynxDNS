package resolver

import (
	"context"
	"fmt"
	"net"
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
	ActionCacheHit   Action = "cache_hit"
	ActionDomestic   Action = "domestic"
	ActionRemote     Action = "remote"
	ActionAdBlock    Action = "ad_block"
	ActionLeakBlock  Action = "leak_block"
	ActionRuleBlock  Action = "rule_block"
	ActionRedirect   Action = "redirected"
	ActionDefault    Action = "default"
	ActionFailed     Action = "failed"
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

type queryLogCfg struct {
	queryLevel       string
	cacheLogInterval int
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

	queryLogConfig   atomic.Value
	cacheLogTimes    sync.Map
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
	r.queryLogConfig.Store(queryLogCfg{
		queryLevel:       cfg.Log.QueryLevel,
		cacheLogInterval: cfg.Log.CacheLogInterval,
	})

	r.initUpstreams()
	return r
}

func (r *Resolver) UpdateConfig(cfg *config.FullConfig) {
	r.mu.Lock()
	r.cfg = cfg
	r.queryLogConfig.Store(queryLogCfg{
		queryLevel:       cfg.Log.QueryLevel,
		cacheLogInterval: cfg.Log.CacheLogInterval,
	})
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
			ResultIPs:   extractIPsFromMsg(cached),
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
			r.incrementMap(&r.byAction, string(ActionRuleBlock))
			r.emitLog(QueryLog{
				Timestamp:   time.Now().UTC(),
				Domain:      domain,
				Type:        dns.TypeToString[q.Qtype],
				ClientIP:    clientAddr,
				Action:      ActionRuleBlock,
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
		r.incrementMap(&r.byAction, string(ActionAdBlock))
		r.emitLog(QueryLog{
			Timestamp:   time.Now().UTC(),
			Domain:      domain,
			Type:        dns.TypeToString[q.Qtype],
			ClientIP:    clientAddr,
			Action:      ActionAdBlock,
			MatchedRule: "ad_filter",
		})
		return r.blockResponse(msg)
	}

	remoteCategories := cfg.Routing.Geosite.Remote
	domesticCategories := cfg.Routing.Geosite.Domestic

	if cfg.Advanced.LeakProtection.Mode == "loose" || !cfg.Advanced.LeakProtection.Enabled {
		if len(domesticCategories) > 0 && r.geoManager.MatchGeosite(domain, domesticCategories) {
			return r.forwardToGroup(msg, domain, q, clientAddr, cfg.DNS.Domestic, ActionDomestic, "geosite:matched")
		}
		if len(remoteCategories) > 0 && r.geoManager.MatchGeosite(domain, remoteCategories) {
			return r.forwardToGroup(msg, domain, q, clientAddr, cfg.DNS.Remote, ActionRemote, "geosite:matched")
		}
	} else {
		if len(remoteCategories) > 0 && r.geoManager.MatchGeosite(domain, remoteCategories) {
			return r.forwardToGroup(msg, domain, q, clientAddr, cfg.DNS.Remote, ActionRemote, "geosite:matched")
		}
		if len(domesticCategories) > 0 && r.geoManager.MatchGeosite(domain, domesticCategories) {
			return r.forwardToGroup(msg, domain, q, clientAddr, cfg.DNS.Domestic, ActionDomestic, "geosite:matched")
		}
	}

	adFilterCategories := cfg.Routing.Geosite.AdFilter
	if cfg.Advanced.AdFilter.Enabled && len(adFilterCategories) > 0 && r.geoManager.MatchGeosite(domain, adFilterCategories) {
		atomic.AddInt64(&r.blockedQueries, 1)
		r.incrementMap(&r.byAction, string(ActionAdBlock))
		r.emitLog(QueryLog{
			Timestamp:   time.Now().UTC(),
			Domain:      domain,
			Type:        dns.TypeToString[q.Qtype],
			ClientIP:    clientAddr,
			Action:      ActionAdBlock,
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
				r.incrementMap(&r.byAction, string(ActionLeakBlock))
				r.emitLog(QueryLog{
					Timestamp:   time.Now().UTC(),
					Domain:      domain,
					Type:        dns.TypeToString[q.Qtype],
					ClientIP:    clientAddr,
					Action:      ActionLeakBlock,
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
		r.incrementMap(&r.byStatus, "timeout")
		r.incrementMap(&r.byAction, string(ActionFailed))
		r.emitLog(QueryLog{
			Timestamp:  time.Now().UTC(),
			Domain:     domain,
			Type:       dns.TypeToString[q.Qtype],
			ClientIP:   clientAddr,
			ServerUsed: fmt.Sprintf("%v", servers),
			LatencyMs:  float64(result.Latency.Microseconds()) / 1000.0,
			Action:     ActionFailed,
			MatchedRule: result.Error.Error(),
		})
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
	msg.SetEdns0(uint16(xclient.DefaultEDNSSize), false)

	ql := &QueryLog{
		Timestamp: time.Now().UTC(),
		Domain:    domain,
		Type:      dns.TypeToString[qtype],
	}

	result := r.ruleEngine.Match(domain)
	if result.Matched {
		switch result.RuleType {
		case rules.TypeBlock:
			ql.Action = ActionRuleBlock
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
		ql.Action = ActionAdBlock
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
		ql.Action = ActionAdBlock
		ql.MatchedRule = "geosite:ad_filter"
		return ql, nil
	}

	if cfg.Advanced.LeakProtection.Enabled && !isLocalDomain(domain) {
		hasRemote := len(remoteCategories) > 0 && r.geoManager.HasGeositeCategories(remoteCategories)
		hasDomestic := len(domesticCategories) > 0 && r.geoManager.HasGeositeCategories(domesticCategories)

		switch cfg.Advanced.LeakProtection.Mode {
		case "strict":
			if hasRemote && hasDomestic {
				ql.Action = ActionLeakBlock
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

func actionLabel(a Action) string {
	switch a {
	case ActionDomestic:
		return "国内"
	case ActionRemote:
		return "远程"
	case ActionCacheHit:
		return "缓存"
	case ActionAdBlock:
		return "广告拦截"
	case ActionLeakBlock:
		return "防泄漏"
	case ActionRuleBlock:
		return "规则拦截"
	case ActionRedirect:
		return "重定向"
	case ActionDefault:
		return "默认"
	case ActionFailed:
		return "超时"
	default:
		return string(a)
	}
}

func isIP(s string) bool {
	return net.ParseIP(s) != nil
}

func hasTypePrefix(s string) bool {
	return strings.HasPrefix(s, "HTTPS:") ||
		strings.HasPrefix(s, "SVCB:") ||
		strings.HasPrefix(s, "MX:") ||
		strings.HasPrefix(s, "TXT:") ||
		strings.HasPrefix(s, "NS:")
}

func formatResults(ips []string) string {
	if len(ips) == 0 {
		return "(无记录)"
	}
	var cnames []string
	var addrs []string
	var others []string
	for _, ip := range ips {
		if isIP(ip) {
			addrs = append(addrs, ip)
		} else if hasTypePrefix(ip) {
			others = append(others, ip)
		} else {
			cnames = append(cnames, ip)
		}
	}
	var parts []string
	if len(cnames) > 0 {
		parts = append(parts, "CNAME:"+strings.Join(cnames, ","))
	}
	if len(addrs) > 0 {
		if len(addrs) > 3 {
			parts = append(parts, addrs[0]+",+"+fmt.Sprintf("%d", len(addrs)-1))
		} else {
			parts = append(parts, strings.Join(addrs, ","))
		}
	}
	parts = append(parts, others...)
	return strings.Join(parts, " | ")
}

func formatRule(rule string) string {
	if rule == "" || rule == "geosite:matched" || rule == "default_policy" {
		return ""
	}
	return rule
}

func (r *Resolver) emitLog(ql QueryLog) {
	qlc := r.queryLogConfig.Load().(queryLogCfg)
	queryLevel := qlc.queryLevel
	cacheLogInterval := qlc.cacheLogInterval

	shouldLog := queryLevel != "off"

	if shouldLog && ql.Action == ActionCacheHit && cacheLogInterval > 0 {
		key := ql.Type + ":" + ql.Domain
		now := time.Now()
		if last, ok := r.cacheLogTimes.Load(key); ok {
			if now.Sub(last.(time.Time)) < time.Duration(cacheLogInterval)*time.Second {
				shouldLog = false
			} else {
				r.cacheLogTimes.Store(key, now)
			}
		} else {
			if cacheLogInterval > 0 {
				var count int
				r.cacheLogTimes.Range(func(_, _ interface{}) bool {
					count++
					return count < 8192
				})
				if count >= 8192 {
					r.cacheLogTimes.Range(func(k, v interface{}) bool {
						if now.Sub(v.(time.Time)) > time.Duration(cacheLogInterval)*time.Second {
							r.cacheLogTimes.Delete(k)
						}
						return true
					})
				}
			}
			r.cacheLogTimes.Store(key, now)
		}
	}

	if shouldLog {
		var color string
		switch ql.Action {
		case ActionDomestic:
			color = xlog.ColorDomestic
		case ActionRemote:
			color = xlog.ColorRemote
		case ActionCacheHit:
			color = xlog.ColorCacheHit
		case ActionAdBlock:
			color = xlog.ColorAdBlock
		case ActionLeakBlock:
			color = xlog.ColorLeakBlock
		case ActionRuleBlock:
			color = xlog.ColorRuleBlock
		case ActionFailed:
			color = xlog.ColorFailed
		case ActionRedirect:
			color = xlog.ColorRedirect
		case ActionDefault:
			color = xlog.ColorDefault
		default:
			color = xlog.ColorDefault
		}

		label := actionLabel(ql.Action)
		labelColored := color + label + xlog.ColorDefault

		switch ql.Action {
		case ActionCacheHit:
			results := formatResults(ql.ResultIPs)
			if results == "(无记录)" {
				xlog.ColorInfoMulti(color, "%s  %s", labelColored, ql.Domain)
			} else {
				xlog.ColorInfoMulti(color, "%s  %s → %s  cache", labelColored, ql.Domain, results)
			}
		case ActionAdBlock, ActionLeakBlock, ActionRuleBlock:
			rule := formatRule(ql.MatchedRule)
			if rule != "" {
				xlog.ColorInfoMulti(color, "%s  %s  [%s]", labelColored, ql.Domain, rule)
			} else {
				xlog.ColorInfoMulti(color, "%s  %s", labelColored, ql.Domain)
			}
		case ActionFailed:
			xlog.ColorInfoMulti(color, "%s  %s  %.0fms %s  %s", labelColored, ql.Domain, ql.LatencyMs, ql.ServerUsed, ql.MatchedRule)
		default:
			results := formatResults(ql.ResultIPs)
			rule := formatRule(ql.MatchedRule)
			var debugExtra string
			if queryLevel == "debug" {
				parts := make([]string, 0, 2)
				if ql.TTL > 0 {
					parts = append(parts, fmt.Sprintf("TTL=%d", ql.TTL))
				}
				if ql.ClientIP != "" {
					parts = append(parts, "client="+ql.ClientIP)
				}
				if len(parts) > 0 {
					debugExtra = "  " + strings.Join(parts, " ")
				}
			}
			if rule != "" {
				xlog.ColorInfoMulti(color, "%s  %s → %s  %.0fms %s  [%s]%s", labelColored, ql.Domain, results, ql.LatencyMs, ql.ServerUsed, rule, debugExtra)
			} else {
				xlog.ColorInfoMulti(color, "%s  %s → %s  %.0fms %s%s", labelColored, ql.Domain, results, ql.LatencyMs, ql.ServerUsed, debugExtra)
			}
		}
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
		case *dns.HTTPS:
			if v.Priority == 0 {
				ips = append(ips, "HTTPS:alias "+v.Target)
			} else {
				ips = append(ips, "HTTPS:"+v.Target)
			}
		case *dns.SVCB:
			if v.Priority == 0 {
				ips = append(ips, "SVCB:alias "+v.Target)
			} else {
				ips = append(ips, "SVCB:"+v.Target)
			}
		case *dns.MX:
			ips = append(ips, "MX:"+v.Mx)
		case *dns.TXT:
			if len(v.Txt) > 0 {
				ips = append(ips, "TXT:"+strings.Join(v.Txt, " "))
			}
		case *dns.NS:
			ips = append(ips, "NS:"+v.Ns)
		}
	}
	return ips
}
