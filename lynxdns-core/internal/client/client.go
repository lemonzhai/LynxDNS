package client

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	xlog "github.com/lynxdns/lynxdns-core/internal/log"
	"github.com/lynxdns/lynxdns-core/internal/upstream"
)

// ErrCircuitOpen 表示上游熔断器处于 open 状态，请求被拒绝。
var ErrCircuitOpen = errors.New("upstream circuit open")

type Protocol string

const (
	ProtoUDP Protocol = "udp"
	ProtoTCP Protocol = "tcp"
	ProtoDoT Protocol = "tls"
	ProtoDoH Protocol = "https"
)

const (
	DefaultEDNSSize uint16 = 1232
	MaxEDNSSize     uint16 = 4096
)

type UpstreamConfig struct {
	Address     string
	Protocol    Protocol
	ServerName  string
	Timeout     time.Duration
}

type Result struct {
	Msg        *dns.Msg
	ServerUsed string
	Latency    time.Duration
	Error      error
}

type DNSClient struct {
	mu       sync.RWMutex
	upstreams map[string]*upstreamClient
	timeout   time.Duration
	httpClient *http.Client
}

type upstreamClient struct {
	config   UpstreamConfig
	dnsClient *dns.Client
	conn      net.Conn
	stats     *upstream.Stats    // 按上游维度的统计
	breaker   *upstream.Breaker  // 熔断器
}

func New(timeout time.Duration) *DNSClient {
	return &DNSClient{
		upstreams: make(map[string]*upstreamClient),
		timeout:   timeout,
		httpClient: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: false,
				},
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     30 * time.Second,
			},
		},
	}
}

func ParseUpstream(addr string) UpstreamConfig {
	cfg := UpstreamConfig{
		Timeout: 5 * time.Second,
	}

	addr = strings.TrimSpace(addr)

	switch {
	case strings.HasPrefix(addr, "https://"):
		cfg.Protocol = ProtoDoH
		cfg.Address = addr
		cfg.ServerName = extractHost(addr)
	case strings.HasPrefix(addr, "tls://"):
		cfg.Protocol = ProtoDoT
		trimmed := strings.TrimPrefix(addr, "tls://")
		cfg.Address = trimmed
		cfg.ServerName, _, _ = net.SplitHostPort(trimmed)
	case strings.HasPrefix(addr, "tcp://"):
		cfg.Protocol = ProtoTCP
		cfg.Address = strings.TrimPrefix(addr, "tcp://")
	default:
		cfg.Protocol = ProtoUDP
		if strings.HasPrefix(addr, "udp://") {
			cfg.Address = strings.TrimPrefix(addr, "udp://")
		} else {
			cfg.Address = addr
		}
	}

	return cfg
}

func (c *DNSClient) AddUpstream(addr string) {
	cfg := ParseUpstream(addr)

	uc := &upstreamClient{
		config:  cfg,
		stats:   upstream.NewStats(),
		breaker: upstream.NewBreaker(),
	}

	switch cfg.Protocol {
	case ProtoUDP:
		uc.dnsClient = &dns.Client{
			Net:          "udp",
			ReadTimeout:  c.timeout,
			WriteTimeout: c.timeout,
			UDPSize:      DefaultEDNSSize,
		}
	case ProtoTCP:
		uc.dnsClient = &dns.Client{
			Net:          "tcp",
			ReadTimeout:  c.timeout,
			WriteTimeout: c.timeout,
		}
	case ProtoDoT:
		uc.dnsClient = &dns.Client{
			Net:          "tcp-tls",
			ReadTimeout:  c.timeout,
			WriteTimeout: c.timeout,
			TLSConfig: &tls.Config{
				ServerName: cfg.ServerName,
			},
		}
	case ProtoDoH:
	}

	c.mu.Lock()
	c.upstreams[addr] = uc
	c.mu.Unlock()

	xlog.Info("added upstream DNS: %s (%s)", addr, cfg.Protocol)
}

func (c *DNSClient) RemoveUpstream(addr string) {
	c.mu.Lock()
	delete(c.upstreams, addr)
	c.mu.Unlock()
}

func (c *DNSClient) Query(ctx context.Context, msg *dns.Msg, serverAddr string) *Result {
	c.mu.RLock()
	uc, ok := c.upstreams[serverAddr]
	c.mu.RUnlock()

	if !ok {
		return &Result{
			Error:      fmt.Errorf("unknown upstream: %s", serverAddr),
			ServerUsed: serverAddr,
		}
	}

	// 熔断检查：open 状态拒绝请求；冷却到期迁移到 half-open 放行探针。
	if uc.breaker != nil && !uc.breaker.AllowRequest() {
		return &Result{
			Error:      ErrCircuitOpen,
			ServerUsed: serverAddr,
		}
	}

	start := time.Now()

	var result *Result
	switch uc.config.Protocol {
	case ProtoDoH:
		result = c.queryDoH(ctx, msg, uc, start)
	default:
		result = c.queryDNS(ctx, msg, uc, start)
	}

	// 统计与熔断更新（与 DNS 收发解耦，recover 兜底绝不影响主路径）
	c.recordResult(uc, result)

	return result
}

// recordResult 根据请求结果更新统计与熔断器状态。
// isTimeout 通过错误类型判定：context.DeadlineExceeded 或 os.IsTimeout。
func (c *DNSClient) recordResult(uc *upstreamClient, result *Result) {
	defer func() { _ = recover() }()
	if uc == nil || uc.stats == nil || uc.breaker == nil || result == nil {
		return
	}

	if result.Error == nil {
		uc.stats.RecordSuccess(result.Latency)
		uc.breaker.RecordSuccess()
		return
	}

	// 熔断拒绝本身不计入失败统计（避免熔断期间的拒绝进一步累加失败）
	if errors.Is(result.Error, ErrCircuitOpen) {
		return
	}

	isTimeout := errors.Is(result.Error, context.DeadlineExceeded) || os.IsTimeout(result.Error)
	uc.stats.RecordFailure(isTimeout)
	uc.breaker.RecordFailure()
}

func (c *DNSClient) QueryGroup(ctx context.Context, msg *dns.Msg, servers []string, concurrency int) *Result {
	if len(servers) == 0 {
		return &Result{Error: fmt.Errorf("no servers provided")}
	}

	if concurrency > len(servers) {
		concurrency = len(servers)
	}

	selected := servers[:concurrency]

	if len(selected) == 1 {
		return c.Query(ctx, msg, selected[0])
	}

	results := make(chan *Result, len(selected))
	for _, addr := range selected {
		go func(a string) {
			results <- c.Query(ctx, msg, a)
		}(addr)
	}

	var best *Result
	for i := 0; i < len(selected); i++ {
		r := <-results
		if r.Error == nil {
			if best == nil || r.Latency < best.Latency {
				best = r
			}
		}
	}

	if best == nil {
		return &Result{
			Error:      fmt.Errorf("all upstream servers failed"),
			ServerUsed: servers[0],
		}
	}

	return best
}

func (c *DNSClient) queryDNS(ctx context.Context, msg *dns.Msg, uc *upstreamClient, start time.Time) *Result {
	addr := ensurePort(uc.config.Address, "53")

	r, _, err := uc.dnsClient.ExchangeContext(ctx, msg, addr)
	latency := time.Since(start)

	if err != nil {
		xlog.Debug("DNS query to %s failed: %v", addr, err)
		return &Result{
			Error:      err,
			Latency:    latency,
			ServerUsed: uc.config.Address,
		}
	}

	return &Result{
		Msg:        r,
		Latency:    latency,
		ServerUsed: uc.config.Address,
	}
}

func (c *DNSClient) queryDoH(ctx context.Context, msg *dns.Msg, uc *upstreamClient, start time.Time) *Result {
	packed, err := msg.Pack()
	if err != nil {
		return &Result{Error: err, ServerUsed: uc.config.Address}
	}

	url := strings.TrimSuffix(uc.config.Address, "/") + "?dns=" + dnsMsgToBase64URL(packed)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return &Result{Error: err, ServerUsed: uc.config.Address}
	}
	req.Header.Set("Accept", "application/dns-message")

	resp, err := c.httpClient.Do(req)
	latency := time.Since(start)

	if err != nil {
		xlog.Debug("DoH query to %s failed: %v", uc.config.Address, err)
		return &Result{
			Error:      err,
			Latency:    latency,
			ServerUsed: uc.config.Address,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &Result{
			Error:      fmt.Errorf("DoH server returned %d: %s", resp.StatusCode, string(body)),
			Latency:    latency,
			ServerUsed: uc.config.Address,
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &Result{Error: err, Latency: latency, ServerUsed: uc.config.Address}
	}

	r := new(dns.Msg)
	if err := r.Unpack(body); err != nil {
		return &Result{Error: err, Latency: latency, ServerUsed: uc.config.Address}
	}

	return &Result{
		Msg:        r,
		Latency:    latency,
		ServerUsed: uc.config.Address,
	}
}

func (c *DNSClient) SetTimeout(timeout time.Duration) {
	c.mu.Lock()
	c.timeout = timeout
	c.httpClient.Timeout = timeout
	c.mu.Unlock()
}

type UpstreamStatusInfo struct {
	// 旧字段（保留，向后兼容）
	Status       string  `json:"status"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	// 新增：统计
	Requests         int64   `json:"requests"`
	Successes        int64   `json:"successes"`
	Failures         int64   `json:"failures"`
	Timeouts         int64   `json:"timeouts"`
	P95LatencyMs     float64 `json:"p95_latency_ms"`
	P99LatencyMs     float64 `json:"p99_latency_ms"`
	AvgLatencyMsHist float64 `json:"avg_latency_ms_hist"`
	// 新增：熔断
	CurrentState string `json:"current_state"` // closed/open/half_open
	TripCount    int64  `json:"trip_count"`
	RecoverCount int64  `json:"recover_count"`
	// 新增：协议
	Protocol string `json:"protocol"`
}

func (c *DNSClient) UpstreamStatuses() map[string]*UpstreamStatusInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	statuses := make(map[string]*UpstreamStatusInfo)
	for addr, uc := range c.upstreams {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		msg := new(dns.Msg)
		msg.SetQuestion(".", dns.TypeNS)
		msg.RecursionDesired = true
		msg.SetEdns0(uint16(DefaultEDNSSize), false)

		start := time.Now()
		result := c.Query(ctx, msg, addr)
		latency := time.Since(start)
		cancel()

		info := &UpstreamStatusInfo{}

		// 即时探测状态（up/down）
		if result.Error != nil {
			info.Status = "down"
			info.AvgLatencyMs = 0
		} else {
			info.Status = "up"
			info.AvgLatencyMs = float64(latency.Microseconds()) / 1000.0
		}

		// 合并统计与熔断快照
		if uc.stats != nil {
			snap := uc.stats.Snapshot()
			info.Requests = snap.Requests
			info.Successes = snap.Successes
			info.Failures = snap.Failures
			info.Timeouts = snap.Timeouts
			info.P95LatencyMs = snap.P95LatencyMs
			info.P99LatencyMs = snap.P99LatencyMs
			info.AvgLatencyMsHist = snap.AvgLatencyMs
		}
		if uc.breaker != nil {
			bsnap := uc.breaker.Snapshot()
			info.CurrentState = bsnap.CurrentState
			info.TripCount = bsnap.TripCount
			info.RecoverCount = bsnap.RecoverCount
		}
		if uc.config.Protocol != "" {
			info.Protocol = string(uc.config.Protocol)
		}

		statuses[addr] = info
	}
	return statuses
}

func ensurePort(addr, defaultPort string) string {
	_, _, err := net.SplitHostPort(addr)
	if err != nil {
		return net.JoinHostPort(addr, defaultPort)
	}
	return addr
}

func extractHost(urlStr string) string {
	urlStr = strings.TrimPrefix(urlStr, "https://")
	urlStr = strings.TrimPrefix(urlStr, "http://")
	parts := strings.SplitN(urlStr, "/", 2)
	host := parts[0]
	host = strings.TrimSuffix(host, ":443")
	host = strings.TrimSuffix(host, ":80")
	return host
}

func dnsMsgToBase64URL(data []byte) string {
	const encodeURL = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

	var buf []byte
	for i := 0; i < len(data); i += 3 {
		b0, b1, b2 := byte(0), byte(0), byte(0)
		remaining := len(data) - i
		b0 = data[i]
		if remaining > 1 {
			b1 = data[i+1]
		}
		if remaining > 2 {
			b2 = data[i+2]
		}

		buf = append(buf,
			encodeURL[b0>>2],
			encodeURL[(b0&0x03)<<4|(b1>>4)],
		)
		if remaining > 1 {
			buf = append(buf, encodeURL[(b1&0x0f)<<2|(b2>>6)])
		}
		if remaining > 2 {
			buf = append(buf, encodeURL[b2&0x3f])
		}
	}

	return string(buf)
}
