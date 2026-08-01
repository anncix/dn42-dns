package cache

import (
	"container/list"
	"sync"
	"time"

	"github.com/miekg/dns"
	"smartdns/internal/config"
)

// DnsCache DNS缓存
type DnsCache struct {
	cfg      *config.CacheConfig
	mu       sync.RWMutex
	lruList  *list.List
	lruMap   map[string]*list.Element
	stats    CacheStats
}

// CacheStats 缓存统计
type CacheStats struct {
	TotalQueries uint64
	CacheHits    uint64
	CacheMisses  uint64
}

// cacheEntry 缓存条目
type cacheEntry struct {
	key        string
	msg        *dns.Msg
	expiresAt  time.Time
	staleUntil time.Time // 惰性缓存过期时间
	hitCount   int
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
	now := time.Now()

	// 检查是否过期
	if now.Before(entry.expiresAt) {
		// 命中，移到前面
		c.lruList.MoveToFront(elem)
		entry.hitCount++
		c.stats.CacheHits++

		// 复制响应并更新TTL
		resp := entry.msg.Copy()
		remaining := uint32(time.Until(entry.expiresAt).Seconds())
		updateTTL(resp, remaining)

		// 设置ID
		resp.Id = r.Id
		return resp, true
	}

	// 惰性缓存检查
	if c.cfg.LazyCache && now.Before(entry.staleUntil) {
		c.lruList.MoveToFront(elem)
		entry.hitCount++
		c.stats.CacheHits++

		resp := entry.msg.Copy()
		// 惰性缓存返回1秒TTL
		updateTTL(resp, 1)
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

	now := time.Now()
	entry := &cacheEntry{
		key:        key,
		msg:        resp.Copy(),
		expiresAt:  now.Add(time.Duration(ttl) * time.Second),
		staleUntil: now.Add(time.Duration(ttl+c.cfg.LazyCacheTTL) * time.Second),
		hitCount:   1,
	}

	// 检查是否已存在
	if elem, ok := c.lruMap[key]; ok {
		// 更新
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

// ShouldPrefetch 判断是否需要预取
func (c *DnsCache) ShouldPrefetch(r *dns.Msg) bool {
	if !c.cfg.Prefetch {
		return false
	}

	key := cacheKey(r)
	if key == "" {
		return false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	elem, ok := c.lruMap[key]
	if !ok {
		return false
	}

	entry := elem.Value.(*cacheEntry)

	// 命中次数超过阈值且即将过期（剩余时间小于TTL的20%）
	if entry.hitCount >= c.cfg.PrefetchThreshold {
		remaining := time.Until(entry.expiresAt)
		totalDuration := entry.staleUntil.Sub(entry.expiresAt.Add(-time.Duration(c.cfg.LazyCacheTTL) * time.Second))
		return remaining < totalDuration/5
	}

	return false
}
