package cache

import (
	"container/list"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

type CacheEntry struct {
	Key        string
	Msg        *dns.Msg
	ExpireAt   time.Time
	ServerUsed string
	CreatedAt  time.Time
}

type Stats struct {
	TotalEntries int     `json:"total_entries"`
	MaxSize      int     `json:"max_size"`
	Hits         int64   `json:"hits"`
	Misses       int64   `json:"misses"`
	HitRate      float64 `json:"hit_rate"`
}

type DNSCache struct {
	mu       sync.RWMutex
	entries  map[string]*list.Element
	lruList  *list.List
	maxSize  int
	lazy     bool
	hits     int64
	misses   int64
}

func New(maxSize int, lazy bool) *DNSCache {
	return &DNSCache{
		entries: make(map[string]*list.Element, maxSize),
		lruList: list.New(),
		maxSize: maxSize,
		lazy:    lazy,
	}
}

func key(domain string, qtype uint16) string {
	return strings.ToLower(dns.Fqdn(domain)) + "/" + dns.TypeToString[qtype]
}

func (c *DNSCache) Get(domain string, qtype uint16) (*dns.Msg, bool) {
	k := key(domain, qtype)
	c.mu.RLock()
	elem, ok := c.entries[k]
	c.mu.RUnlock()

	if !ok {
		c.mu.Lock()
		c.misses++
		c.mu.Unlock()
		return nil, false
	}

	entry := elem.Value.(*CacheEntry)

	now := time.Now()
	if now.After(entry.ExpireAt) {
		if c.lazy {
			c.mu.Lock()
			c.hits++
			c.lruList.MoveToFront(elem)
			c.mu.Unlock()
			return copyMsg(entry.Msg), true
		}
		c.mu.Lock()
		c.misses++
		delete(c.entries, entry.Key)
		c.lruList.Remove(elem)
		c.mu.Unlock()
		return nil, false
	}

	c.mu.Lock()
	c.hits++
	c.lruList.MoveToFront(elem)
	c.mu.Unlock()
	return copyMsg(entry.Msg), true
}

func (c *DNSCache) Set(domain string, qtype uint16, msg *dns.Msg, serverUsed string) {
	if msg == nil || len(msg.Answer) == 0 {
		return
	}

	ttl := extractMinTTL(msg)
	if ttl == 0 {
		return
	}

	k := key(domain, qtype)
	entry := &CacheEntry{
		Key:        k,
		Msg:        copyMsg(msg),
		ExpireAt:   time.Now().Add(time.Duration(ttl) * time.Second),
		ServerUsed: serverUsed,
		CreatedAt:  time.Now(),
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.entries[k]; ok {
		elem.Value = entry
		c.lruList.MoveToFront(elem)
		return
	}

	if len(c.entries) >= c.maxSize {
		c.evictOne()
	}

	elem := c.lruList.PushFront(entry)
	c.entries[k] = elem
}

func (c *DNSCache) IsLazy() bool {
	return c.lazy
}

func (c *DNSCache) IsExpired(domain string, qtype uint16) (bool, error) {
	k := key(domain, qtype)
	c.mu.RLock()
	elem, ok := c.entries[k]
	c.mu.RUnlock()

	if !ok {
		return false, fmt.Errorf("cache entry not found: %s", k)
	}
	entry := elem.Value.(*CacheEntry)
	return time.Now().After(entry.ExpireAt), nil
}

func (c *DNSCache) Delete(domain string, qtype uint16) {
	k := key(domain, qtype)
	c.mu.Lock()
	if elem, ok := c.entries[k]; ok {
		c.lruList.Remove(elem)
		delete(c.entries, k)
	}
	c.mu.Unlock()
}

func (c *DNSCache) Clear() int {
	c.mu.Lock()
	count := len(c.entries)
	c.entries = make(map[string]*list.Element, c.maxSize)
	c.lruList.Init()
	c.hits = 0
	c.misses = 0
	c.mu.Unlock()
	return count
}

func (c *DNSCache) Stats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := c.hits + c.misses
	var hitRate float64
	if total > 0 {
		hitRate = float64(c.hits) / float64(total) * 100
	}

	return Stats{
		TotalEntries: len(c.entries),
		MaxSize:      c.maxSize,
		Hits:         c.hits,
		Misses:       c.misses,
		HitRate:      hitRate,
	}
}

func (c *DNSCache) Entries(page, pageSize int, domainFilter string) ([]CacheEntryView, int) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var all []CacheEntryView
	for e := c.lruList.Front(); e != nil; e = e.Next() {
		entry := e.Value.(*CacheEntry)
		parts := strings.SplitN(entry.Key, "/", 2)
		domain := parts[0]
		qtype := ""
		if len(parts) > 1 {
			qtype = parts[1]
		}

		if domainFilter != "" && !strings.Contains(strings.ToLower(domain), strings.ToLower(domainFilter)) {
			continue
		}

		ips := extractIPs(entry.Msg)
		ttl := entry.ExpireAt.Sub(time.Now())
		if ttl < 0 {
			ttl = 0
		}

		all = append(all, CacheEntryView{
			Domain:       domain,
			Type:         qtype,
			TTL:          int(ttl.Seconds()),
			RemainingTTL: int(ttl.Seconds()),
			ExpireAt:     entry.ExpireAt,
			Value:        ips,
			ServerUsed:   entry.ServerUsed,
		})
	}

	total := len(all)
	start := (page - 1) * pageSize
	if start >= total {
		return []CacheEntryView{}, total
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	return all[start:end], total
}

type CacheEntryView struct {
	Domain       string    `json:"domain"`
	Type         string    `json:"type"`
	TTL          int       `json:"ttl"`
	RemainingTTL int       `json:"remaining_ttl"`
	ExpireAt     time.Time `json:"expire_at"`
	Value        []string  `json:"value"`
	ServerUsed   string    `json:"server_used"`
}

func (c *DNSCache) evictOne() {
	oldest := c.lruList.Back()
	if oldest != nil {
		entry := oldest.Value.(*CacheEntry)
		delete(c.entries, entry.Key)
		c.lruList.Remove(oldest)
	}
}

func copyMsg(msg *dns.Msg) *dns.Msg {
	return msg.Copy()
}

func extractMinTTL(msg *dns.Msg) uint32 {
	var minTTL uint32 = ^uint32(0)
	found := false
	for _, rr := range msg.Answer {
		if rr != nil {
			ttl := rr.Header().Ttl
			if ttl < minTTL {
				minTTL = ttl
			}
			found = true
		}
	}
	if !found {
		return 0
	}
	return minTTL
}

func extractIPs(msg *dns.Msg) []string {
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
