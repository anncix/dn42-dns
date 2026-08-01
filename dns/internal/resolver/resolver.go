package resolver

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
	"smartdns/internal/config"
)

const (
	// 失败后冷却时间（秒），这段时间内跳过该服务器
	coolDownSeconds = 30
	// 滑动窗口大小，用于计算平均延迟
	latencyWindowSize = 10
	// 健康检查探测间隔（成功后多久再探测一次失败的服务器，这里用冷却时间控制）
)

// Manager DNS解析器管理器
type Manager struct {
	groups map[string]*ResolverGroup
}

// ResolverGroup 解析器组
type ResolverGroup struct {
	servers []*Resolver
	mu      sync.RWMutex
}

// Resolver 单个解析器
type Resolver struct {
	Addr     string
	Name     string
	Protocol string // udp / tcp
	client   *dns.Client

	// 健康状态
	healthy     int32     // 1=健康, 0=不健康（原子操作）
	lastFail    time.Time // 最后一次失败时间
	lastSuccess time.Time // 最后一次成功时间
	failCount   int32     // 连续失败次数（原子操作）

	// 延迟统计（滑动窗口）
	latencies   []time.Duration
	latencyIdx  int
	latencyMu   sync.Mutex
	avgLatency  time.Duration // 缓存的平均延迟（原子读，写需加锁）
}

// NewManager 创建解析器管理器
func NewManager(cfg *config.UpstreamConfig) (*Manager, error) {
	m := &Manager{
		groups: make(map[string]*ResolverGroup),
	}

	// 创建默认组
	defaultGroup, err := createGroup(cfg.Default)
	if err != nil {
		return nil, fmt.Errorf("创建默认上游组失败: %w", err)
	}
	m.groups["default"] = defaultGroup

	// 创建其他组
	for name, servers := range cfg.Groups {
		group, err := createGroup(servers)
		if err != nil {
			return nil, fmt.Errorf("创建上游组 %s 失败: %w", name, err)
		}
		m.groups[name] = group
	}

	return m, nil
}

// createGroup 创建解析器组
func createGroup(servers []config.UpstreamServer) (*ResolverGroup, error) {
	group := &ResolverGroup{}

	for _, s := range servers {
		r, err := newResolver(s)
		if err != nil {
			return nil, err
		}
		group.servers = append(group.servers, r)
	}

	if len(group.servers) == 0 {
		return nil, fmt.Errorf("上游服务器列表为空")
	}

	return group, nil
}

// newResolver 创建单个解析器
func newResolver(s config.UpstreamServer) (*Resolver, error) {
	proto := s.Protocol
	if proto == "" {
		proto = "udp"
	}

	switch proto {
	case "udp", "tcp":
		r := &Resolver{
			Addr:      s.Addr,
			Name:      s.Name,
			Protocol:  proto,
			healthy:   1, // 默认健康
			latencies: make([]time.Duration, latencyWindowSize),
			client: &dns.Client{
				Net:     proto,
				Timeout: 2 * time.Second,
			},
		}
		return r, nil
	default:
		return nil, fmt.Errorf("不支持的协议: %s (仅支持 udp/tcp)", proto)
	}
}

// isHealthy 检查服务器是否健康（考虑冷却时间）
func (r *Resolver) isHealthy() bool {
	if atomic.LoadInt32(&r.healthy) == 1 {
		return true
	}

	// 不健康时，检查是否过了冷却期
	lastFail := r.lastFail
	if time.Since(lastFail) > time.Duration(coolDownSeconds)*time.Second {
		// 冷却期过了，尝试恢复（设置为健康，让它试一次）
		atomic.StoreInt32(&r.healthy, 1)
		return true
	}

	return false
}

// markSuccess 标记成功
func (r *Resolver) markSuccess(latency time.Duration) {
	atomic.StoreInt32(&r.healthy, 1)
	atomic.StoreInt32(&r.failCount, 0)
	r.lastSuccess = time.Now()

	// 更新延迟统计
	r.latencyMu.Lock()
	r.latencies[r.latencyIdx] = latency
	r.latencyIdx = (r.latencyIdx + 1) % latencyWindowSize

	// 重新计算平均（只算有值的）
	var total time.Duration
	var count int
	for _, d := range r.latencies {
		if d > 0 {
			total += d
			count++
		}
	}
	if count > 0 {
		r.avgLatency = total / time.Duration(count)
	}
	r.latencyMu.Unlock()
}

// markFail 标记失败
func (r *Resolver) markFail() {
	atomic.AddInt32(&r.failCount, 1)
	r.lastFail = time.Now()

	// 连续失败1次就标记为不健康（进入冷却）
	atomic.StoreInt32(&r.healthy, 0)
}

// getAvgLatency 获取平均延迟
func (r *Resolver) getAvgLatency() time.Duration {
	r.latencyMu.Lock()
	defer r.latencyMu.Unlock()
	return r.avgLatency
}

// Resolve 解析DNS请求
func (m *Manager) Resolve(r *dns.Msg, group string) (*dns.Msg, error) {
	g, ok := m.groups[group]
	if !ok {
		g = m.groups["default"]
	}

	if g == nil {
		return nil, fmt.Errorf("上游组 %s 不存在", group)
	}

	// 获取服务器列表，按健康状态 + 延迟排序
	servers := g.getSortedServers()

	var lastErr error
	for _, server := range servers {
		resp, latency, err := server.queryWithLatency(r)
		if err == nil {
			server.markSuccess(latency)

			// 如果UDP响应被截断，用TCP重试获取完整响应
			if server.Protocol == "udp" && resp.Truncated {
				tcpClient := &dns.Client{Net: "tcp", Timeout: 2 * time.Second}
				tcpResp, _, err2 := tcpClient.Exchange(r, server.Addr)
				if err2 == nil {
					return tcpResp, nil
				}
			}
			return resp, nil
		}
		server.markFail()
		lastErr = err
	}

	return nil, fmt.Errorf("所有上游服务器均失败: %w", lastErr)
}

// getSortedServers 获取排序后的服务器列表
// 排序规则：健康的在前，健康的按平均延迟升序，不健康的放最后
func (g *ResolverGroup) getSortedServers() []*Resolver {
	g.mu.RLock()
	servers := make([]*Resolver, len(g.servers))
	copy(servers, g.servers)
	g.mu.RUnlock()

	// 简单的冒泡排序（服务器数量少，没问题）
	n := len(servers)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			a, b := servers[j], servers[j+1]
			aHealthy := a.isHealthy()
			bHealthy := b.isHealthy()

			// 健康的排在前面
			if aHealthy && !bHealthy {
				continue // a 已经在前面
			}
			if !aHealthy && bHealthy {
				servers[j], servers[j+1] = b, a
				continue
			}
			// 都健康：按平均延迟排序（延迟低的在前）
			if aHealthy && bHealthy {
				aLat := a.getAvgLatency()
				bLat := b.getAvgLatency()
				// 没有延迟数据的视为相等
				if aLat > 0 && bLat > 0 && aLat > bLat {
					servers[j], servers[j+1] = b, a
				}
			}
			// 都不健康：保持原有顺序
		}
	}

	return servers
}

// queryWithLatency 发送DNS查询并返回延迟
func (r *Resolver) queryWithLatency(msg *dns.Msg) (*dns.Msg, time.Duration, error) {
	start := time.Now()
	resp, _, err := r.client.Exchange(msg, r.Addr)
	latency := time.Since(start)
	return resp, latency, err
}

// GetGroupNames 获取所有组名
func (m *Manager) GetGroupNames() []string {
	names := make([]string, 0, len(m.groups))
	for name := range m.groups {
		names = append(names, name)
	}
	return names
}

// GetServerStats 获取服务器状态统计（用于 API 和监控）
type ServerStats struct {
	Name      string `json:"name"`
	Addr      string `json:"addr"`
	Protocol  string `json:"protocol"`
	Healthy   bool   `json:"healthy"`
	AvgLatMs  int64  `json:"avg_latency_ms"`
	FailCount int32  `json:"fail_count"`
}

// GetGroupStats 获取组内所有服务器状态
func (m *Manager) GetGroupStats(group string) []ServerStats {
	g, ok := m.groups[group]
	if !ok {
		return nil
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	stats := make([]ServerStats, 0, len(g.servers))
	for _, s := range g.servers {
		stats = append(stats, ServerStats{
			Name:      s.Name,
			Addr:      s.Addr,
			Protocol:  s.Protocol,
			Healthy:   s.isHealthy(),
			AvgLatMs:  s.getAvgLatency().Milliseconds(),
			FailCount: atomic.LoadInt32(&s.failCount),
		})
	}
	return stats
}
