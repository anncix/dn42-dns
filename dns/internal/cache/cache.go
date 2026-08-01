package cache

import (
	"container/list"
	"fmt"
	"sync"
	"time"

	"github.com/miekg/dns"
	"smartdns/internal/config"
)

// DnsCache DNS缓存
type DnsCache struct {
	cfg     *config.CacheConfig
	mu      sync.RWMutex
	lruList *list.List
	lruMap  map[string]*list.Element
	stats   CacheStats
}

// CacheStats 缓存统计
type CacheStats struct {
	TotalQueries uint64
	CacheHits    uint64
	CacheMisses  uint64
}

// cacheEntry 缓存条目
type cacheEntry struct {
	key       string
	msg       *dns.Msg
	expiresAt time.Time
}

// NewDnsCache 创建DNS缓存
func NewDnsCache(cfg *config.CacheConfig) *DnsCache {
	if !cfg.Enabled {
		return nil
	}

	return &DnsCache{
		cfg:     cfg,
		lruList: list.New(),
		lruMap:  make(map[string]*list.Element),
	}
}

// cacheKey 生成缓存键
func cacheKey(r *dns.Msg) string {
	if len(r.Question) == 0 {
		return ""
	}
	q := r.Question[0]
	return q.Name + ":" + dns.TypeToString[q.Qtype] + ":" + dns.ClassToString[q.Qclass]
}

// Get 从缓存获取响应
func (c *DnsCache) Get(r *dns.Msg) (*dns.Msg, bool) {
	if c == nil {
		return nil, false
	}

	key := cacheKey(r)
	if key == "" {
		return nil, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.stats.TotalQueries++

	elem, ok := c.lruMap[key]
	if !ok {
		c.stats.CacheMisses++
		return nil, false
	}

	entry := elem.Value.(*cacheEntry)

	// 检查是否过期
	if time.Now().Before(entry.expiresAt) {
		// 命中，移到前面
		c.lruList.MoveToFront(elem)
		c.stats.CacheHits++

		// 复制响应并更新TTL
		resp := entry.msg.Copy()
		remaining := uint32(time.Until(entry.expiresAt).Seconds())
		updateTTL(resp, remaining)
		resp.Id = r.Id
		return resp, true
	}

	// 已过期，删除
	c.removeElement(elem)
	c.stats.CacheMisses++
	return nil, false
}

// Set 存入缓存
func (c *DnsCache) Set(r *dns.Msg, resp *dns.Msg) {
	if c == nil {
		return
	}

	key := cacheKey(r)
	if key == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// 计算TTL
	ttl := c.calculateTTL(resp)
	if ttl == 0 {
		return
	}

	entry := &cacheEntry{
		key:       key,
		msg:       resp.Copy(),
		expiresAt: time.Now().Add(time.Duration(ttl) * time.Second),
	}

	// 检查是否已存在
	if elem, ok := c.lruMap[key]; ok {
		c.lruList.MoveToFront(elem)
		elem.Value = entry
		return
	}

	// 新增
	elem := c.lruList.PushFront(entry)
	c.lruMap[key] = elem

	// 检查容量，淘汰最久未使用的
	if c.lruList.Len() > c.cfg.MaxSize {
		c.evictOldest()
	}
}

// calculateTTL 计算响应的TTL
func (c *DnsCache) calculateTTL(resp *dns.Msg) uint32 {
	// NXDOMAIN响应使用否定缓存TTL
	if resp.Rcode == dns.RcodeNameError {
		return c.cfg.NegTTL
	}

	// 从应答中找最小TTL
	var minTTL uint32 = 0
	allRRs := append(resp.Answer, resp.Ns...)
	allRRs = append(allRRs, resp.Extra...)

	for _, rr := range allRRs {
		ttl := rr.Header().Ttl
		if minTTL == 0 || ttl < minTTL {
			minTTL = ttl
		}
	}

	if minTTL == 0 {
		return 0
	}

	// 应用TTL范围限制
	if minTTL < c.cfg.MinTTL {
		minTTL = c.cfg.MinTTL
	}
	if minTTL > c.cfg.MaxTTL {
		minTTL = c.cfg.MaxTTL
	}

	return minTTL
}

// updateTTL 更新响应中所有记录的TTL
func updateTTL(resp *dns.Msg, ttl uint32) {
	for _, rr := range resp.Answer {
		rr.Header().Ttl = ttl
	}
	for _, rr := range resp.Ns {
		rr.Header().Ttl = ttl
	}
	for _, rr := range resp.Extra {
		rr.Header().Ttl = ttl
	}
}

// removeElement 移除元素
func (c *DnsCache) removeElement(elem *list.Element) {
	entry := elem.Value.(*cacheEntry)
	delete(c.lruMap, entry.key)
	c.lruList.Remove(elem)
}

// evictOldest 淘汰最久未使用的
func (c *DnsCache) evictOldest() {
	elem := c.lruList.Back()
	if elem != nil {
		c.removeElement(elem)
	}
}

// GetStats 获取缓存统计
func (c *DnsCache) GetStats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stats
}

// GetSize 获取缓存条目数
func (c *DnsCache) GetSize() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lruList.Len()
}

// Flush 清空缓存
func (c *DnsCache) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lruList.Init()
	c.lruMap = make(map[string]*list.Element)
}

// StatsSummary 获取统计摘要
func (c *DnsCache) StatsSummary() string {
	if c == nil {
		return "缓存: 未启用"
	}
	stats := c.GetStats()
	size := c.GetSize()
	hitRate := float64(0)
	if stats.TotalQueries > 0 {
		hitRate = float64(stats.CacheHits) / float64(stats.TotalQueries) * 100
	}
	return fmt.Sprintf("缓存: %d 条目, 命中%d/总%d (%.1f%%)",
		size, stats.CacheHits, stats.TotalQueries, hitRate)
}
