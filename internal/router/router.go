package router

import (
	"strings"
	"sync"

	"smartdns/internal/config"
	"smartdns/internal/filter"
)

// Router DNS分流路由器
type Router struct {
	cfg       *config.RoutingConfig
	suffixMap map[string]string // 后缀 -> 组名
	exactMap  map[string]string // 精确域名 -> 组名
	mu        sync.RWMutex
}

// NewRouter 创建分流路由器
func NewRouter(cfg *config.RoutingConfig) *Router {
	r := &Router{
		cfg:       cfg,
		suffixMap: make(map[string]string),
		exactMap:  make(map[string]string),
	}

	// 加载后缀规则
	for group, suffixes := range cfg.DomainSuffix {
		for _, suffix := range suffixes {
			r.suffixMap[strings.ToLower(suffix)] = group
		}
	}

	// 加载精确规则
	for group, domains := range cfg.DomainExact {
		for _, domain := range domains {
			r.exactMap[strings.ToLower(domain)] = group
		}
	}

	return r
}

// Route 确定域名应该走哪个上游组
func (r *Router) Route(domain string) string {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))

	r.mu.RLock()
	defer r.mu.RUnlock()

	// 1. 精确匹配
	if group, ok := r.exactMap[domain]; ok {
		return group
	}

	// 2. 后缀匹配（从最长到最短尝试）
	bestGroup := ""
	bestLen := 0

	for suffix, group := range r.suffixMap {
		// 支持两种格式: .example.com 或 example.com
		cleanSuffix := strings.TrimPrefix(suffix, ".")
		// 匹配: domain == cleanSuffix (域名本身) 或 domain 以 .cleanSuffix 结尾 (子域名)
		if domain == cleanSuffix || strings.HasSuffix(domain, "."+cleanSuffix) {
			if len(suffix) > bestLen {
				bestLen = len(suffix)
				bestGroup = group
			}
		}
	}

	if bestGroup != "" {
		return bestGroup
	}

	// 3. 返回默认组
	return r.cfg.DefaultGroup
}

// Reload 重新加载规则
func (r *Router) Reload(cfg *config.RoutingConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.cfg = cfg
	r.suffixMap = make(map[string]string)
	r.exactMap = make(map[string]string)

	for group, suffixes := range cfg.DomainSuffix {
		for _, suffix := range suffixes {
			r.suffixMap[strings.ToLower(suffix)] = group
		}
	}

	for group, domains := range cfg.DomainExact {
		for _, domain := range domains {
			r.exactMap[strings.ToLower(domain)] = group
		}
	}
}

// GetStats 获取路由统计（预留接口）
func (r *Router) GetStats() map[string]uint64 {
	// TODO: 实现路由统计
	return nil
}

// 确保filter包被引用（防止编译错误）
var _ = filter.NewRadixTree
