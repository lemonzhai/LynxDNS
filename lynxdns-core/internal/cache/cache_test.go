package cache

import (
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func newDNSMsg(domain string, ip string, ttl uint32) *dns.Msg {
	m := new(dns.Msg)
	m.SetReply(&dns.Msg{})
	m.Answer = append(m.Answer, &dns.A{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(domain),
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    ttl,
		},
		A: parseIP(ip),
	})
	return m
}

func parseIP(s string) net.IP {
	ip := net.ParseIP(s)
	if ip == nil {
		return net.IPv4(127, 0, 0, 1)
	}
	return ip.To4()
}

func TestCacheSetAndGet(t *testing.T) {
	c := New(100, false)

	msg := newDNSMsg("example.com", "1.2.3.4", 300)
	c.Set("example.com", dns.TypeA, msg, "udp://8.8.8.8:53")

	got, ok := c.Get("example.com", dns.TypeA)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if len(got.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(got.Answer))
	}
}

func TestCacheMiss(t *testing.T) {
	c := New(100, false)

	_, ok := c.Get("nonexistent.com", dns.TypeA)
	if ok {
		t.Error("expected cache miss")
	}
}

func TestCacheExpiry(t *testing.T) {
	c := New(100, false)

	msg := newDNSMsg("expire.com", "1.2.3.4", 1)
	c.Set("expire.com", dns.TypeA, msg, "test")

	_, ok := c.Get("expire.com", dns.TypeA)
	if !ok {
		t.Error("expected cache hit before expiry")
	}

	time.Sleep(1100 * time.Millisecond)

	_, ok = c.Get("expire.com", dns.TypeA)
	if ok {
		t.Error("expected expired entry to be a miss when lazy=false")
	}
}

func TestCacheLazyMode(t *testing.T) {
	c := New(100, true)

	msg := newDNSMsg("lazy.com", "1.2.3.4", 1)
	c.Set("lazy.com", dns.TypeA, msg, "test")

	time.Sleep(1100 * time.Millisecond)

	got, ok := c.Get("lazy.com", dns.TypeA)
	if !ok {
		t.Error("expected lazy cache to return expired entry")
	}
	if got == nil {
		t.Error("expected non-nil response")
	}
}

func TestCacheClear(t *testing.T) {
	c := New(100, false)

	for i := 0; i < 10; i++ {
		domain := "test" + string(rune('0'+i)) + ".com"
		msg := newDNSMsg(domain, "1.2.3.4", 300)
		c.Set(domain, dns.TypeA, msg, "test")
	}

	stats := c.Stats()
	if stats.TotalEntries != 10 {
		t.Errorf("expected 10 entries, got %d", stats.TotalEntries)
	}

	count := c.Clear()
	if count != 10 {
		t.Errorf("expected 10 cleared entries, got %d", count)
	}

	stats = c.Stats()
	if stats.TotalEntries != 0 {
		t.Errorf("expected 0 entries after clear, got %d", stats.TotalEntries)
	}
}

func TestCacheEviction(t *testing.T) {
	c := New(3, false)

	for i := 0; i < 5; i++ {
		domain := "evict" + string(rune('0'+i)) + ".com"
		msg := newDNSMsg(domain, "1.2.3.4", 300)
		c.Set(domain, dns.TypeA, msg, "test")
	}

	stats := c.Stats()
	if stats.TotalEntries > 3 {
		t.Errorf("expected max 3 entries, got %d", stats.TotalEntries)
	}
}

func TestCacheStats(t *testing.T) {
	c := New(100, false)

	msg := newDNSMsg("stats.com", "1.2.3.4", 300)
	c.Set("stats.com", dns.TypeA, msg, "test")

	c.Get("stats.com", dns.TypeA)
	c.Get("stats.com", dns.TypeA)
	c.Get("miss.com", dns.TypeA)

	stats := c.Stats()
	if stats.Hits != 2 {
		t.Errorf("expected 2 hits, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", stats.Misses)
	}
	if stats.MaxSize != 100 {
		t.Errorf("expected max size 100, got %d", stats.MaxSize)
	}
}

func TestCacheDelete(t *testing.T) {
	c := New(100, false)

	msg := newDNSMsg("delete.com", "1.2.3.4", 300)
	c.Set("delete.com", dns.TypeA, msg, "test")

	_, ok := c.Get("delete.com", dns.TypeA)
	if !ok {
		t.Fatal("expected cache hit before delete")
	}

	c.Delete("delete.com", dns.TypeA)

	_, ok = c.Get("delete.com", dns.TypeA)
	if ok {
		t.Error("expected cache miss after delete")
	}
}

func TestCacheCaseInsensitive(t *testing.T) {
	c := New(100, false)

	msg := newDNSMsg("Example.COM", "1.2.3.4", 300)
	c.Set("Example.COM", dns.TypeA, msg, "test")

	_, ok := c.Get("example.com", dns.TypeA)
	if !ok {
		t.Error("expected case-insensitive cache hit")
	}
}

func TestCacheEntries(t *testing.T) {
	c := New(100, false)

	for i := 0; i < 5; i++ {
		domain := "page" + string(rune('0'+i)) + ".com"
		msg := newDNSMsg(domain, "1.2.3.4", 300)
		c.Set(domain, dns.TypeA, msg, "test")
	}

	entries, total := c.Entries(1, 3, "")
	if total != 5 {
		t.Errorf("expected total 5, got %d", total)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries on page 1, got %d", len(entries))
	}

	entries2, _ := c.Entries(2, 3, "")
	if len(entries2) != 2 {
		t.Errorf("expected 2 entries on page 2, got %d", len(entries2))
	}
}

func TestCacheIsExpired(t *testing.T) {
	c := New(100, true)

	msg := newDNSMsg("expired.com", "1.2.3.4", 1)
	c.Set("expired.com", dns.TypeA, msg, "test")

	time.Sleep(1100 * time.Millisecond)

	expired, err := c.IsExpired("expired.com", dns.TypeA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !expired {
		t.Error("expected entry to be expired")
	}
}
