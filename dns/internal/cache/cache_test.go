package cache

import (
	"fmt"
	"net"
	"testing"

	"github.com/miekg/dns"
	"smartdns/internal/config"
)

func newTestCache() *DnsCache {
	cfg := &config.CacheConfig{
		Enabled: true,
		MaxSize: 100,
		MinTTL:  10,
		MaxTTL:  3600,
		NegTTL:  60,
	}
	return NewDnsCache(cfg)
}

func makeQuery(domain string, qtype uint16) *dns.Msg {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(domain), qtype)
	return m
}

func makeAResponse(query *dns.Msg, ip string, ttl uint32) *dns.Msg {
	resp := new(dns.Msg)
	resp.SetReply(query)
	rr := &dns.A{
		Hdr: dns.RR_Header{
			Name:   query.Question[0].Name,
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    ttl,
		},
		A: net.ParseIP(ip),
	}
	resp.Answer = append(resp.Answer, rr)
	return resp
}

func TestCache_SetAndGet(t *testing.T) {
	c := newTestCache()
	if c == nil {
		t.Fatal("缓存不应该为nil")
	}

	query := makeQuery("example.com", dns.TypeA)
	resp := makeAResponse(query, "1.2.3.4", 300)

	c.Set(query, resp)

	result, ok := c.Get(query)
	if !ok {
		t.Fatal("缓存应该命中")
	}
	if len(result.Answer) != 1 {
		t.Fatalf("应答数 = %d, 期望 1", len(result.Answer))
	}
	a := result.Answer[0].(*dns.A)
	if a.A.String() != "1.2.3.4" {
		t.Errorf("IP = %s, 期望 1.2.3.4", a.A.String())
	}
}

func TestCache_NegativeCache(t *testing.T) {
	c := newTestCache()

	query := makeQuery("nonexistent.example.com", dns.TypeA)
	resp := new(dns.Msg)
	resp.SetReply(query)
	resp.Rcode = dns.RcodeNameError // NXDOMAIN

	c.Set(query, resp)

	result, ok := c.Get(query)
	if !ok {
		t.Fatal("否定缓存应该命中")
	}
	if result.Rcode != dns.RcodeNameError {
		t.Errorf("Rcode = %d, 期望 NXDOMAIN(%d)", result.Rcode, dns.RcodeNameError)
	}
}

func TestCache_Miss(t *testing.T) {
	c := newTestCache()

	query := makeQuery("unknown.com", dns.TypeA)
	_, ok := c.Get(query)
	if ok {
		t.Error("不应该命中缓存")
	}
}

func TestCache_Flush(t *testing.T) {
	c := newTestCache()

	query := makeQuery("example.com", dns.TypeA)
	resp := makeAResponse(query, "1.2.3.4", 300)
	c.Set(query, resp)

	if c.GetSize() != 1 {
		t.Fatalf("缓存大小 = %d, 期望 1", c.GetSize())
	}

	c.Flush()

	if c.GetSize() != 0 {
		t.Errorf("清空后缓存大小 = %d, 期望 0", c.GetSize())
	}
}

func TestCache_LRU_Eviction(t *testing.T) {
	cfg := &config.CacheConfig{
		Enabled: true,
		MaxSize: 3,
		MinTTL:  10,
		MaxTTL:  3600,
		NegTTL:  60,
	}
	c := NewDnsCache(cfg)

	for i := 0; i < 4; i++ {
		domain := fmt.Sprintf("test%d.com", i)
		query := makeQuery(domain, dns.TypeA)
		resp := makeAResponse(query, "1.2.3.4", 300)
		c.Set(query, resp)
	}

	if c.GetSize() != 3 {
		t.Errorf("缓存大小 = %d, 期望 3", c.GetSize())
	}
}

func TestCache_Disabled(t *testing.T) {
	cfg := &config.CacheConfig{Enabled: false}
	c := NewDnsCache(cfg)
	if c != nil {
		t.Error("禁用缓存时应该返回nil")
	}
}

func TestCache_Stats(t *testing.T) {
	c := newTestCache()

	query := makeQuery("example.com", dns.TypeA)
	resp := makeAResponse(query, "1.2.3.4", 300)

	// 第一次查询：未命中
	c.Get(query)

	// 存入缓存
	c.Set(query, resp)

	// 第二、三次查询：命中
	c.Get(query)
	c.Get(query)

	stats := c.GetStats()
	if stats.TotalQueries != 3 {
		t.Errorf("总查询 = %d, 期望 3", stats.TotalQueries)
	}
	if stats.CacheHits != 2 {
		t.Errorf("命中 = %d, 期望 2", stats.CacheHits)
	}
	if stats.CacheMisses != 1 {
		t.Errorf("未命中 = %d, 期望 1", stats.CacheMisses)
	}
}

func TestStatsSummary(t *testing.T) {
	c := newTestCache()

	query := makeQuery("example.com", dns.TypeA)
	resp := makeAResponse(query, "1.2.3.4", 300)
	c.Set(query, resp)
	c.Get(query)

	summary := c.StatsSummary()
	if summary == "" {
		t.Error("StatsSummary 不应该为空")
	}
}

func TestCache_TTL_Clamp(t *testing.T) {
	cfg := &config.CacheConfig{
		Enabled: true,
		MaxSize: 100,
		MinTTL:  60,   // 最小60秒
		MaxTTL:  300,  // 最大300秒
		NegTTL:  60,
	}
	c := NewDnsCache(cfg)

	// TTL 太小，应该被提升到 MinTTL
	query := makeQuery("short-ttl.com", dns.TypeA)
	resp := makeAResponse(query, "1.2.3.4", 10) // 10秒 < 60秒
	c.Set(query, resp)

	result, ok := c.Get(query)
	if !ok {
		t.Fatal("缓存应该命中")
	}
	ttl := result.Answer[0].Header().Ttl
	if ttl < 50 || ttl > 60 { // 允许一点误差
		t.Errorf("TTL = %d, 期望接近 60 (MinTTL)", ttl)
	}
}
