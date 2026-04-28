package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	xlog "github.com/lynxdns/lynxdns-core/internal/log"
)

type Protocol string

const (
	ProtoUDP Protocol = "udp"
	ProtoTCP Protocol = "tcp"
	ProtoDoT Protocol = "tls"
	ProtoDoH Protocol = "https"
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
		config: cfg,
	}

	switch cfg.Protocol {
	case ProtoUDP:
		uc.dnsClient = &dns.Client{
			Net:          "udp",
			ReadTimeout:  c.timeout,
			WriteTimeout: c.timeout,
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

	start := time.Now()

	switch uc.config.Protocol {
	case ProtoDoH:
		return c.queryDoH(ctx, msg, uc, start)
	default:
		return c.queryDNS(ctx, msg, uc, start)
	}
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
	Status       string  `json:"status"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
}

func (c *DNSClient) UpstreamStatuses() map[string]*UpstreamStatusInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	statuses := make(map[string]*UpstreamStatusInfo)
	for addr := range c.upstreams {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		msg := new(dns.Msg)
		msg.SetQuestion(".", dns.TypeNS)
		msg.RecursionDesired = true

		start := time.Now()
		result := c.Query(ctx, msg, addr)
		latency := time.Since(start)
		cancel()

		if result.Error != nil {
			statuses[addr] = &UpstreamStatusInfo{Status: "down", AvgLatencyMs: 0}
		} else {
			statuses[addr] = &UpstreamStatusInfo{
				Status:       "up",
				AvgLatencyMs: float64(latency.Microseconds()) / 1000.0,
			}
		}
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
