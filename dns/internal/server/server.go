package server

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
	"smartdns/internal/api"
	"smartdns/internal/cache"
	"smartdns/internal/config"
	"smartdns/internal/ha"
	"smartdns/internal/mail"
	"smartdns/internal/rdns"
	"smartdns/internal/resolver"
	"smartdns/internal/router"
)

// Server DNS服务器
type Server struct {
	cfg      *config.Config
	cache    *cache.DnsCache
	resolver *resolver.Manager
	router   *router.Router
	rdns     *rdns.Handler
	mail     *mail.Handler
	api      *api.Server
	ha       *ha.Manager
	servers  []*dns.Server
	stats    ServerStats
	mu       sync.RWMutex
	running  bool
	stopCh   chan struct{}
	startTime time.Time
}

// ServerStats 服务器统计
type ServerStats struct {
	TotalQueries uint64
	CacheHits    uint64
	Forwarded    uint64
	Failed       uint64
}

// NewServer 创建DNS服务器
func NewServer(cfg *config.Config) (*Server, error) {
	s := &Server{
		cfg:    cfg,
		stopCh: make(chan struct{}),
	}

	// 初始化缓存
	s.cache = cache.NewDnsCache(&cfg.Cache)

	// 初始化解析器
	resolverMgr, err := resolver.NewManager(&cfg.Upstreams)
	if err != nil {
		return nil, fmt.Errorf("初始化上游管理器失败: %w", err)
	}
	s.resolver = resolverMgr

	// 初始化路由器
	s.router = router.NewRouter(&cfg.Routing)

	// 初始化rDNS处理器
	rdnsHandler, err := rdns.NewHandler(&cfg.Rdns)
	if err != nil {
		return nil, fmt.Errorf("初始化rDNS处理器失败: %w", err)
	}
	s.rdns = rdnsHandler

	// 初始化邮局DNS处理器
	mailHandler, err := mail.NewHandler(&cfg.Mail)
	if err != nil {
		return nil, fmt.Errorf("初始化邮局DNS处理器失败: %w", err)
	}
	s.mail = mailHandler

	// 初始化HTTP API
	if cfg.API.Enabled {
		apiSrv := api.NewServer(&cfg.API)
		apiSrv.SetCache(s.cache)
		apiSrv.SetRdns(s.rdns)
		apiSrv.SetMail(s.mail)
		apiSrv.SetRouter(s.router)
		apiSrv.SetResolver(s.resolver)
		// 设置 HA 模式（即使 standalone 也设置，便于 API 显示）
		haMode := cfg.HA.Mode
		if haMode == "" {
			haMode = "standalone"
		}
		apiSrv.SetHAMode(haMode)
		s.api = apiSrv
	}

	// 初始化高可用管理器
	if cfg.HA.Mode != "standalone" && cfg.HA.Mode != "" {
		haMgr := ha.NewManager(&cfg.HA)
		haMgr.SetRdns(s.rdns)
		s.ha = haMgr
	}

	return s, nil
}

// Start 启动DNS服务器
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("服务器已在运行")
	}

	s.startTime = time.Now()

	for _, listen := range s.cfg.Listen {
		srv := &dns.Server{
			Addr: listen.Addr,
			Net:  listen.Protocol,
			Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
				s.handleRequest(w, r)
			}),
		}
		s.servers = append(s.servers, srv)

		go func(srv *dns.Server) {
			if err := srv.ListenAndServe(); err != nil {
				fmt.Printf("[错误] %s 监听失败: %v\n", srv.Net, err)
			}
		}(srv)

		fmt.Printf("监听 %s/%s\n", listen.Addr, listen.Protocol)
	}

	// 启动 HTTP API
	if s.api != nil {
		if err := s.api.Start(); err != nil {
			fmt.Printf("[警告] API 启动失败: %v\n", err)
		}
	}

	// 启动 HA 管理器
	if s.ha != nil {
		if err := s.ha.Start(); err != nil {
			fmt.Printf("[警告] HA 启动失败: %v\n", err)
		}
		// 启动 HA 状态同步到 API 的协程
		go s.haStatusSyncLoop()
	}

	s.running = true
	return nil
}

// haStatusSyncLoop 定期同步 HA 状态到 API
func (s *Server) haStatusSyncLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if s.ha == nil || s.api == nil {
				return
			}
			mode := s.ha.Mode()
			var status interface{}
			if mode == "master" {
				status = map[string]interface{}{
					"mode":   mode,
					"slaves": s.ha.GetSlaveStatus(),
				}
			} else if mode == "slave" {
				status = map[string]interface{}{
					"mode":   mode,
					"master": s.ha.GetMasterStatus(),
				}
			} else {
				status = map[string]string{"mode": mode}
			}
			s.api.SetHAStatus(status)
		case <-s.stopCh:
			return
		}
	}
}

// Stop 停止DNS服务器
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	close(s.stopCh)

	// 停止 HA
	if s.ha != nil {
		s.ha.Stop()
	}

	// 停止 API
	if s.api != nil {
		s.api.Stop()
	}

	// 停止 DNS 服务器
	for _, srv := range s.servers {
		srv.Shutdown()
	}
	s.running = false
}

// FlushCache 清空缓存
func (s *Server) FlushCache() {
	if s.cache != nil {
		s.cache.Flush()
		fmt.Println("缓存已清空")
	}
}

// handleRequest 处理DNS请求
func (s *Server) handleRequest(w dns.ResponseWriter, r *dns.Msg) {
	defer func() {
		if rec := recover(); rec != nil {
			fmt.Printf("[错误] Panic恢复: %v\n", rec)
			m := new(dns.Msg)
			m.SetRcode(r, dns.RcodeServerFailure)
			w.WriteMsg(m)
		}
	}()

	if r == nil || len(r.Question) == 0 {
		return
	}

	atomic.AddUint64(&s.stats.TotalQueries, 1)
	q := r.Question[0]
	clientIP := getClientIP(w)

	// PTR查询处理
	if q.Qtype == dns.TypePTR && s.rdns != nil {
		resp, shouldForward := s.rdns.HandlePTR(r, clientIP)
		if resp != nil {
			if s.cfg.Log.QueryLog {
				fmt.Printf("[rDNS-本地] %s\n", q.Name)
			}
			w.WriteMsg(resp)
			return
		}
		if !shouldForward {
			// 丢弃（不在允许网段）
			if s.cfg.Log.QueryLog {
				fmt.Printf("[rDNS-丢弃] %s\n", q.Name)
			}
			m := new(dns.Msg)
			m.SetRcode(r, dns.RcodeRefused)
			w.WriteMsg(m)
			return
		}
	}

	// 邮局DNS查询处理（MX/TXT/A 本地记录）
	if s.mail != nil {
		resp, handled := s.mail.HandleQuery(r)
		if handled && resp != nil {
			if s.cfg.Log.QueryLog {
				fmt.Printf("[邮局DNS-本地] %s %s\n", dns.TypeToString[q.Qtype], q.Name)
			}
			w.WriteMsg(resp)
			return
		}
	}

	// 缓存查询
	if s.cache != nil {
		if resp, ok := s.cache.Get(r); ok {
			atomic.AddUint64(&s.stats.CacheHits, 1)
			if s.cfg.Log.QueryLog {
				fmt.Printf("[缓存命中] %s %s\n", dns.TypeToString[q.Qtype], q.Name)
			}
			w.WriteMsg(resp)
			return
		}
	}

	// 路由选择上游组
	group := s.router.Route(q.Name)

	// 转发查询
	resp, err := s.resolver.Resolve(r, group)
	if err != nil {
		atomic.AddUint64(&s.stats.Failed, 1)
		fmt.Printf("[错误] 解析失败 %s: %v\n", q.Name, err)
		m := new(dns.Msg)
		m.SetRcode(r, dns.RcodeServerFailure)
		w.WriteMsg(m)
		return
	}

	atomic.AddUint64(&s.stats.Forwarded, 1)

	// 存入缓存
	if s.cache != nil {
		s.cache.Set(r, resp)
	}

	if s.cfg.Log.QueryLog {
		fmt.Printf("[%s] %s %s\n", group, dns.TypeToString[q.Qtype], q.Name)
	}

	w.WriteMsg(resp)
}

// getClientIP 获取客户端IP
func getClientIP(w dns.ResponseWriter) string {
	addr := w.RemoteAddr()
	if addr == nil {
		return ""
	}
	switch a := addr.(type) {
	case *net.UDPAddr:
		return a.IP.String()
	case *net.TCPAddr:
		return a.IP.String()
	default:
		parts := strings.Split(addr.String(), ":")
		if len(parts) >= 2 {
			return strings.Join(parts[:len(parts)-1], ":")
		}
		return addr.String()
	}
}

// PrintStats 打印统计信息
func (s *Server) PrintStats() {
	total := atomic.LoadUint64(&s.stats.TotalQueries)
	cacheHits := atomic.LoadUint64(&s.stats.CacheHits)
	forwarded := atomic.LoadUint64(&s.stats.Forwarded)
	failed := atomic.LoadUint64(&s.stats.Failed)

	fmt.Println("\n=== 统计信息 ===")
	fmt.Printf("总查询: %d\n", total)
	fmt.Printf("缓存命中: %d\n", cacheHits)
	fmt.Printf("转发查询: %d\n", forwarded)
	fmt.Printf("失败: %d\n", failed)

	if total > 0 {
		fmt.Printf("缓存命中率: %.1f%%\n", float64(cacheHits)/float64(total)*100)
	}

	fmt.Println(s.cache.StatsSummary())
	fmt.Println(s.router.StatsSummary())

	if s.rdns != nil && s.cfg.Rdns.Enabled {
		fmt.Println(s.rdns.StatsSummary())
	}

	if s.mail != nil && s.cfg.Mail.Enabled {
		fmt.Println(s.mail.StatsSummary())
	}

	// HA 状态
	haMode := s.cfg.HA.Mode
	if haMode == "" {
		haMode = "standalone"
	}
	fmt.Printf("HA模式: %s\n", haMode)
	if s.ha != nil {
		if haMode == "master" {
			for addr, status := range s.ha.GetSlaveStatus() {
				statusStr := "健康"
				if !status.Healthy {
					statusStr = "异常"
				}
				fmt.Printf("  从节点 %s: %s (%dms)\n", addr, statusStr, status.LatencyMs)
			}
		} else if haMode == "slave" {
			status := s.ha.GetMasterStatus()
			statusStr := "健康"
			if !status.Healthy {
				statusStr = "异常"
			}
			fmt.Printf("  主节点 %s: %s (%dms)\n", status.Addr, statusStr, status.LatencyMs)
		}
	}

	fmt.Println("================")
}

// StartStatsLoop 启动统计循环
func (s *Server) StartStatsLoop(interval int) {
	if interval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(time.Duration(interval) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.PrintStats()
			case <-s.stopCh:
				return
			}
		}
	}()
}
