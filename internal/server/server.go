package server

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
	"smartdns/internal/cache"
	"smartdns/internal/config"
	"smartdns/internal/filter"
	"smartdns/internal/router"
	"smartdns/internal/upstream"
)

// Server DNS服务器
type Server struct {
	cfg      *config.Config
	filter   *filter.AdFilter
	cache    *cache.DnsCache
	upstream *upstream.Manager
	router   *router.Router
	servers  []*dns.Server
	stats    ServerStats
	mu       sync.RWMutex
	running  bool
}

// ServerStats 服务器统计
type ServerStats struct {
	TotalQueries  uint64
	BlockedQueries uint64
	CacheHits     uint64
	CacheMisses   uint64
	Forwarded     uint64
	Failed        uint64
}

// NewServer 创建DNS服务器
func NewServer(cfg *config.Config) (*Server, error) {
	s := &Server{
		cfg: cfg,
	}

	// 初始化过滤器
	adFilter, err := filter.NewAdFilter(&cfg.AdBlock)
	if err != nil {
		return nil, fmt.Errorf("初始化广告过滤器失败: %w", err)
	}
	s.filter = adFilter

	// 初始化缓存
	s.cache = cache.NewDnsCache(&cfg.Cache)

	// 初始化上游管理器
	upstreamMgr, err := upstream.NewManager(&cfg.Upstreams)
	if err != nil {
		return nil, fmt.Errorf("初始化上游管理器失败: %w", err)
	}
	s.upstream = upstreamMgr

	// 初始化路由器
	s.router = router.NewRouter(&cfg.Routing)

	return s, nil
}

// Start 启动DNS服务器
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("服务器已经在运行")
	}

	// 为每个监听配置创建服务器
	for _, listen := range s.cfg.Listen {
		srv := &dns.Server{
			Addr:    listen.Addr,
			Net:     listen.Protocol,
			Handler: s,
		}

		s.servers = append(s.servers, srv)

		// 异步启动
		go func(srv *dns.Server, proto string, addr string) {
			fmt.Printf("[SmartDNS] 启动 %s DNS 服务器，监听 %s\n", proto, addr)
			if err := srv.ListenAndServe(); err != nil {
				fmt.Printf("[SmartDNS] %s 服务器错误: %v\n", proto, err)
			}
		}(srv, listen.Protocol, listen.Addr)
	}

	s.running = true

	// 启动统计输出
	if s.cfg.Log.StatsInterval > 0 {
		go s.statsLoop()
	}

	return nil
}

// Stop 停止DNS服务器
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	for _, srv := range s.servers {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.ShutdownContext(ctx)
	}

	s.running = false
	return nil
}

// ServeDNS 处理DNS请求（dns.Handler接口）
func (s *Server) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	atomic.AddUint64(&s.stats.TotalQueries, 1)

	// 确保响应设置正确
	defer func() {
		if rec := recover(); rec != nil {
			fmt.Printf("[SmartDNS] 恐慌恢复: %v\n", rec)
			dns.HandleFailed(w, r)
		}
	}()

	// 无问题检查
	if r == nil || len(r.Question) == 0 {
		dns.HandleFailed(w, r)
		return
	}

	q := r.Question[0]
	domain := q.Name

	// 查询日志
	if s.cfg.Log.QueryLog.Enabled && !s.cfg.Log.QueryLog.OnlyBlocked {
		fmt.Printf("[查询] %s %s 来自 %s\n",
			dns.TypeToString[q.Qtype], domain, w.RemoteAddr())
	}

	var resp *dns.Msg
	var err error

	// 处理PTR反向查询
	if q.Qtype == dns.TypePTR && s.cfg.Rdns.Enabled {
		resp = s.handlePTR(r)
		if resp != nil {
			w.WriteMsg(resp)
			return
		}
	}

	// 1. 检查广告拦截
	if s.cfg.AdBlock.Enabled {
		if blocked, rule := s.filter.IsBlocked(domain); blocked {
			atomic.AddUint64(&s.stats.BlockedQueries, 1)
			resp = s.makeBlockResponse(r)

			if s.cfg.Log.QueryLog.Enabled {
				fmt.Printf("[拦截] %s (规则: %s)\n", domain, rule)
			}

			w.WriteMsg(resp)
			return
		}
	}

	// 2. 检查缓存
	if s.cache != nil {
		if cached, ok := s.cache.Get(r); ok {
			atomic.AddUint64(&s.stats.CacheHits, 1)
			w.WriteMsg(cached)
			return
		}
		atomic.AddUint64(&s.stats.CacheMisses, 1)
	}

	// 3. 分流路由
	groupName := s.router.Route(domain)
	group := s.upstream.GetGroup(groupName)

	// 4. 转发查询
	atomic.AddUint64(&s.stats.Forwarded, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, upstreamServer, err := group.Query(ctx, r)
	if err != nil {
		atomic.AddUint64(&s.stats.Failed, 1)
		fmt.Printf("[错误] 查询 %s 失败: %v\n", domain, err)
		dns.HandleFailed(w, r)
		return
	}

	// 设置响应ID
	resp.Id = r.Id

	// 5. 存入缓存
	if s.cache != nil {
		s.cache.Set(r, resp)
	}

	if s.cfg.Log.QueryLog.Enabled && !s.cfg.Log.QueryLog.OnlyBlocked {
		fmt.Printf("[转发] %s -> %s\n", domain, upstreamServer.GetName())
	}

	w.WriteMsg(resp)
}

// handlePTR 处理PTR反向解析请求
func (s *Server) handlePTR(r *dns.Msg) *dns.Msg {
	if len(r.Question) == 0 {
		return nil
	}

	q := r.Question[0]
	ptrName := q.Name

	// 解析IP地址
	ip := ptrToIP(ptrName)
	if ip == nil {
		return nil
	}

	// 检查是否在允许的网段内
	if !s.isAllowedRdnsIP(ip) {
		return nil
	}

	// 查找本地记录
	ipStr := ip.String()
	if domain, ok := s.cfg.Rdns.LocalRecords[ipStr]; ok {
		resp := new(dns.Msg)
		resp.SetReply(r)
		resp.Authoritative = true

		ptr := &dns.PTR{
			Hdr: dns.RR_Header{
				Name:   q.Name,
				Rrtype: dns.TypePTR,
				Class:  dns.ClassINET,
				Ttl:    3600,
			},
			Ptr: dns.Fqdn(domain),
		}
		resp.Answer = append(resp.Answer, ptr)

		if s.cfg.Log.QueryLog.Enabled && !s.cfg.Log.QueryLog.OnlyBlocked {
			fmt.Printf("[rDNS] %s -> %s\n", ipStr, domain)
		}

		return resp
	}

	return nil
}

// ptrToIP 将PTR域名转换为IP地址
func ptrToIP(ptrName string) net.IP {
	ptrName = strings.TrimSuffix(ptrName, ".")

	// IPv4: 1.0.0.127.in-addr.arpa
	if strings.HasSuffix(ptrName, ".in-addr.arpa") {
		parts := strings.Split(strings.TrimSuffix(ptrName, ".in-addr.arpa"), ".")
		if len(parts) == 4 {
			// 反转顺序
			return net.ParseIP(fmt.Sprintf("%s.%s.%s.%s", parts[3], parts[2], parts[1], parts[0]))
		}
	}

	return nil
}

// isAllowedRdnsIP 检查IP是否允许rDNS
func (s *Server) isAllowedRdnsIP(ip net.IP) bool {
	for _, cidr := range s.cfg.Rdns.AllowedNetworks {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// makeBlockResponse 生成拦截响应
func (s *Server) makeBlockResponse(r *dns.Msg) *dns.Msg {
	resp := new(dns.Msg)
	resp.SetReply(r)

	switch s.cfg.AdBlock.BlockMode {
	case "nxdomain":
		resp.Rcode = dns.RcodeNameError
	case "null":
		// 返回0.0.0.0或::
		q := r.Question[0]
		switch q.Qtype {
		case dns.TypeA:
			rr := &dns.A{
				Hdr: dns.RR_Header{
					Name:   q.Name,
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    600,
				},
				A: net.ParseIP(s.cfg.AdBlock.BlockIPv4),
			}
			resp.Answer = append(resp.Answer, rr)
		case dns.TypeAAAA:
			rr := &dns.AAAA{
				Hdr: dns.RR_Header{
					Name:   q.Name,
					Rrtype: dns.TypeAAAA,
					Class:  dns.ClassINET,
					Ttl:    600,
				},
				AAAA: net.ParseIP(s.cfg.AdBlock.BlockIPv6),
			}
			resp.Answer = append(resp.Answer, rr)
		}
	case "nodata":
		// NOERROR + 空应答
		resp.Rcode = dns.RcodeSuccess
	default:
		resp.Rcode = dns.RcodeNameError
	}

	return resp
}

// statsLoop 统计输出循环
func (s *Server) statsLoop() {
	ticker := time.NewTicker(time.Duration(s.cfg.Log.StatsInterval) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		s.printStats()
	}
}

// printStats 输出统计信息
func (s *Server) printStats() {
	total := atomic.LoadUint64(&s.stats.TotalQueries)
	blocked := atomic.LoadUint64(&s.stats.BlockedQueries)
	cacheHits := atomic.LoadUint64(&s.stats.CacheHits)
	cacheMisses := atomic.LoadUint64(&s.stats.CacheMisses)
	forwarded := atomic.LoadUint64(&s.stats.Forwarded)
	failed := atomic.LoadUint64(&s.stats.Failed)

	cacheHitRate := float64(0)
	if cacheHits+cacheMisses > 0 {
		cacheHitRate = float64(cacheHits) / float64(cacheHits+cacheMisses) * 100
	}

	blockRate := float64(0)
	if total > 0 {
		blockRate = float64(blocked) / float64(total) * 100
	}

	fmt.Printf("\n[统计] 总查询: %d | 拦截: %d (%.1f%%) | 缓存命中: %d (%.1f%%) | 转发: %d | 失败: %d\n",
		total, blocked, blockRate, cacheHits, cacheHitRate, forwarded, failed)

	if s.cache != nil {
		fmt.Printf("[缓存] 条目数: %d / %d\n", s.cache.GetSize(), s.cfg.Cache.MaxSize)
	}

	if s.cfg.AdBlock.Enabled {
		fmt.Printf("[过滤] 黑名单: %d 条 | 白名单: %d 条\n",
			s.filter.GetBlacklistSize(), s.filter.GetWhitelistSize())
	}
}

// GetStats 获取统计信息
func (s *Server) GetStats() ServerStats {
	return ServerStats{
		TotalQueries:   atomic.LoadUint64(&s.stats.TotalQueries),
		BlockedQueries: atomic.LoadUint64(&s.stats.BlockedQueries),
		CacheHits:      atomic.LoadUint64(&s.stats.CacheHits),
		CacheMisses:    atomic.LoadUint64(&s.stats.CacheMisses),
		Forwarded:      atomic.LoadUint64(&s.stats.Forwarded),
		Failed:         atomic.LoadUint64(&s.stats.Failed),
	}
}
