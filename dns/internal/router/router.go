package router

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"smartdns/internal/config"
)

// Router DNS分流路由器
type Router struct {
	cfg       *config.RoutingConfig
	suffixMap map[string]string // 后缀 -> 组名
	mu        sync.RWMutex
	stats     map[string]*uint64 // 用指针存atomic计数器
}

// NewRouter 创建分流路由器
func NewRouter(cfg *config.RoutingConfig) *Router {
	r := &Router{
		cfg:       cfg,
		suffixMap: make(map[string]string),
		stats:     make(map[string]*uint64),
	}

	// 加载后缀规则，并为每个组初始化统计计数器
	for group, suffixes := range cfg.DomainSuffix {
		for _, suffix := range suffixes {
			r.suffixMap[strings.ToLower(suffix)] = group
		}
		if _, ok := r.stats[group]; !ok {
			var count uint64
			r.stats[group] = &count
		}
	}

	// 初始化默认组统计
	if _, ok := r.stats[cfg.DefaultGroup]; !ok {
		var count uint64
		r.stats[cfg.DefaultGroup] = &count
	}

	return r
}

// Route 确定域名应该走哪个上游组
func (r *Router) Route(domain string) string {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))

	r.mu.RLock()
	defer r.mu.RUnlock()

	// 后缀匹配（最长匹配优先）
	bestGroup := ""
	bestLen := 0

	for suffix, group := range r.suffixMap {
		cleanSuffix := strings.TrimPrefix(suffix, ".")
		// 匹配域名本身或子域名
		if domain == cleanSuffix || strings.HasSuffix(domain, "."+cleanSuffix) {
			if len(suffix) > bestLen {
				bestLen = len(suffix)
				bestGroup = group
			}
		}
	}

	if bestGroup == "" {
		bestGroup = r.cfg.DefaultGroup
	}

	// 原子递增统计（所有组在初始化时已创建）
	if ptr, ok := r.stats[bestGroup]; ok {
		atomic.AddUint64(ptr, 1)
	}

	return bestGroup
}

// StatsSummary 获取统计摘要
func (r *Router) StatsSummary() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	total := uint64(0)
	for _, v := range r.stats {
		total += atomic.LoadUint64(v)
	}

	if total == 0 {
		return "路由: 0 查询"
	}

	parts := []string{}
	for group, hits := range r.stats {
		h := atomic.LoadUint64(hits)
		pct := float64(h) / float64(total) * 100
		parts = append(parts, fmt.Sprintf("%s=%.0f%%", group, pct))
	}

	return fmt.Sprintf("路由: %d 查询 (%s)", total, strings.Join(parts, ", "))
}
