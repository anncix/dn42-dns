package upstream

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
	"smartdns/internal/config"
)

// Manager 上游DNS管理器
type Manager struct {
	cfg    *config.UpstreamConfig
	groups map[string]*UpstreamGroup
	mu     sync.RWMutex
}

// UpstreamGroup 上游服务器组
type UpstreamGroup struct {
	servers []*Upstream
	index   uint64 // 轮询索引
}

// Upstream 单个上游服务器
type Upstream struct {
	Addr     string
	Name     string
	Protocol string // udp / tcp / dot / doh
	client   *dns.Client
	httpClient *http.Client
	dohURL   string
}

// NewManager 创建上游管理器
func NewManager(cfg *config.UpstreamConfig) (*Manager, error) {
	m := &Manager{
		cfg:    cfg,
		groups: make(map[string]*UpstreamGroup),
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

// createGroup 创建上游组
func createGroup(servers []config.UpstreamServer) (*UpstreamGroup, error) {
	group := &UpstreamGroup{}

	for _, s := range servers {
		u, err := newUpstream(s)
		if err != nil {
			return nil, err
		}
		group.servers = append(group.servers, u)
	}

	if len(group.servers) == 0 {
		return nil, fmt.Errorf("上游服务器列表为空")
	}

	return group, nil
}

// newUpstream 创建单个上游
func newUpstream(s config.UpstreamServer) (*Upstream, error) {
	u := &Upstream{
		Addr:     s.Addr,
		Name:     s.Name,
		Protocol: s.Protocol,
	}

	switch s.Protocol {
	case "udp":
		u.client = &dns.Client{
			Net:     "udp",
			Timeout: 5 * time.Second,
		}
	case "tcp":
		u.client = &dns.Client{
			Net:     "tcp",
			Timeout: 10 * time.Second,
		}
	case "dot":
		u.client = &dns.Client{
			Net:     "tcp-tls",
			Timeout: 10 * time.Second,
			TLSConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		}
	case "doh":
		u.dohURL = s.Addr
		u.httpClient = &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
				},
				MaxIdleConnsPerHost: 10,
			},
		}
	default:
		return nil, fmt.Errorf("不支持的协议: %s", s.Protocol)
	}

	return u, nil
}

// GetGroup 获取上游组
func (m *Manager) GetGroup(name string) *UpstreamGroup {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if group, ok := m.groups[name]; ok {
		return group
	}

	// 返回默认组
	return m.groups["default"]
}

// Query 执行DNS查询（轮询选择上游）
func (g *UpstreamGroup) Query(ctx context.Context, r *dns.Msg) (*dns.Msg, *Upstream, error) {
	if len(g.servers) == 0 {
		return nil, nil, fmt.Errorf("没有可用的上游服务器")
	}

	// 轮询选择起始服务器
	start := atomic.AddUint64(&g.index, 1) - 1
	n := uint64(len(g.servers))

	var lastErr error
	for i := uint64(0); i < n; i++ {
		idx := (start + i) % n
		u := g.servers[idx]

		resp, err := u.Query(ctx, r)
		if err == nil {
			return resp, u, nil
		}
		lastErr = err
	}

	return nil, nil, fmt.Errorf("所有上游服务器查询失败，最后错误: %w", lastErr)
}

// Query 向上游发送查询
func (u *Upstream) Query(ctx context.Context, r *dns.Msg) (*dns.Msg, error) {
	switch u.Protocol {
	case "doh":
		return u.queryDoH(ctx, r)
	default:
		return u.queryDNS(ctx, r)
	}
}

// queryDNS 传统DNS查询（UDP/TCP/DoT）
func (u *Upstream) queryDNS(ctx context.Context, r *dns.Msg) (*dns.Msg, error) {
	if u.client == nil {
		return nil, fmt.Errorf("DNS客户端未初始化")
	}

	// 使用带超时的上下文
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, u.client.Timeout)
		defer cancel()
	}

	resp, _, err := u.client.ExchangeContext(ctx, r, u.Addr)
	if err != nil {
		// UDP失败时尝试TCP
		if u.Protocol == "udp" {
			tcpClient := &dns.Client{
				Net:     "tcp",
				Timeout: 10 * time.Second,
			}
			resp, _, err = tcpClient.ExchangeContext(ctx, r, u.Addr)
		}
	}
	return resp, err
}

// queryDoH DNS-over-HTTPS查询
func (u *Upstream) queryDoH(ctx context.Context, r *dns.Msg) (*dns.Msg, error) {
	if u.httpClient == nil {
		return nil, fmt.Errorf("HTTP客户端未初始化")
	}

	// 编码DNS消息
	packed, err := r.Pack()
	if err != nil {
		return nil, fmt.Errorf("DNS消息编码失败: %w", err)
	}

	// 创建POST请求
	req, err := http.NewRequestWithContext(ctx, "POST", u.dohURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Body = io.NopCloser(
		// 使用bytes.Reader替代
		newBytesReader(packed),
	)
	req.ContentLength = int64(len(packed))

	// 发送请求
	resp, err := u.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DoH请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DoH返回状态码: %d", resp.StatusCode)
	}

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取DoH响应失败: %w", err)
	}

	// 解码DNS响应
	dnsResp := new(dns.Msg)
	if err := dnsResp.Unpack(body); err != nil {
		return nil, fmt.Errorf("DoH响应解码失败: %w", err)
	}

	return dnsResp, nil
}

// bytesReader 简单的bytes.Reader替代
type bytesReader struct {
	data []byte
	pos  int
}

func newBytesReader(data []byte) *bytesReader {
	return &bytesReader{data: data}
}

func (r *bytesReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// GetServers 获取组内所有服务器
func (g *UpstreamGroup) GetServers() []*Upstream {
	return g.servers
}

// Name 获取上游名称
func (u *Upstream) GetName() string {
	if u.Name != "" {
		return u.Name
	}
	return u.Addr
}
