package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"smartdns/internal/cache"
	"smartdns/internal/config"
	"smartdns/internal/mail"
	"smartdns/internal/rdns"
	"smartdns/internal/resolver"
	"smartdns/internal/router"
)

// Server HTTP API 服务器
type Server struct {
	cfg      *config.APIConfig
	cache    *cache.DnsCache
	rdns     *rdns.Handler
	mail     *mail.Handler
	router   *router.Router
	resolver *resolver.Manager
	haMode   string
	haStatus interface{}
	httpSrv  *http.Server
	mu       sync.Mutex
	running  bool
}

// StatsResponse 统计响应
type StatsResponse struct {
	Mode      string            `json:"mode"`
	Cache     cache.CacheStats  `json:"cache"`
	CacheSize int               `json:"cache_size"`
	Rdns      rdns.Stats        `json:"rdns"`
	Upstreams []string          `json:"upstreams"`
}

// HealthResponse 健康检查响应
type HealthResponse struct {
	Status string `json:"status"` // ok
}

// RdnsRecordsResponse rDNS记录响应
type RdnsRecordsResponse struct {
	Records map[string]string `json:"records"`
}

// NewServer 创建 HTTP API 服务器
func NewServer(cfg *config.APIConfig) *Server {
	return &Server{
		cfg: cfg,
	}
}

// SetCache 设置缓存引用
func (s *Server) SetCache(c *cache.DnsCache) {
	s.cache = c
}

// SetRdns 设置 rDNS 处理器引用
func (s *Server) SetRdns(r *rdns.Handler) {
	s.rdns = r
}

// SetMail 设置邮局DNS处理器引用
func (s *Server) SetMail(m *mail.Handler) {
	s.mail = m
}

// SetRouter 设置路由器引用
func (s *Server) SetRouter(r *router.Router) {
	s.router = r
}

// SetResolver 设置解析器引用
func (s *Server) SetResolver(r *resolver.Manager) {
	s.resolver = r
}

// SetHAMode 设置 HA 模式
func (s *Server) SetHAMode(mode string) {
	s.haMode = mode
}

// SetHAStatus 设置 HA 状态
func (s *Server) SetHAStatus(status interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.haStatus = status
}

// Start 启动 HTTP API 服务器
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("API 服务器已在运行")
	}

	mux := http.NewServeMux()

	// 健康检查
	mux.HandleFunc("/api/health", s.handleHealth)

	// 统计信息
	mux.HandleFunc("/api/stats", s.handleStats)

	// 缓存管理
	mux.HandleFunc("/api/cache/flush", s.handleCacheFlush)
	mux.HandleFunc("/api/cache/stats", s.handleCacheStats)

	// rDNS 管理
	mux.HandleFunc("/api/rdns/records", s.handleRdnsRecords)

	// 邮局DNS
	mux.HandleFunc("/api/mail/records", s.handleMailRecords)
	mux.HandleFunc("/api/mail/stats", s.handleMailStats)
	mux.HandleFunc("/api/mail/mx", s.handleMailMX)
	mux.HandleFunc("/api/mail/a", s.handleMailA)
	mux.HandleFunc("/api/mail/spf", s.handleMailSPF)
	mux.HandleFunc("/api/mail/dkim", s.handleMailDKIM)
	mux.HandleFunc("/api/mail/dmarc", s.handleMailDMARC)

	// 上游服务器状态
	mux.HandleFunc("/api/upstreams", s.handleUpstreams)

	// HA 状态
	mux.HandleFunc("/api/ha/status", s.handleHAStatus)

	// 根路径 - 简单状态页
	mux.HandleFunc("/", s.handleIndex)

	s.httpSrv = &http.Server{
		Addr:    s.cfg.Addr,
		Handler: mux,
	}

	go func() {
		fmt.Printf("[API] 监听 %s\n", s.cfg.Addr)
		if err := s.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[API] 错误: %v\n", err)
		}
	}()

	s.running = true
	return nil
}

// Stop 停止 HTTP API 服务器
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	if s.httpSrv != nil {
		s.httpSrv.Close()
	}
	s.running = false
}

// handleIndex 首页
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
    <title>dn42-dns API</title>
    <style>
        body { font-family: -apple-system, sans-serif; max-width: 800px; margin: 40px auto; padding: 0 20px; }
        h1 { color: #333; }
        ul { line-height: 2; }
        code { background: #f4f4f4; padding: 2px 6px; border-radius: 3px; }
    </style>
</head>
<body>
    <h1>dn42-dns API</h1>
    <p>轻量级 DNS 分流服务器 HTTP API</p>
    <h2>API 端点</h2>
    <ul>
        <li><code>GET /api/health</code> - 健康检查</li>
        <li><code>GET /api/stats</code> - 统计信息</li>
        <li><code>POST /api/cache/flush</code> - 清空缓存</li>
        <li><code>GET /api/cache/stats</code> - 缓存统计</li>
        <li><code>GET /api/rdns/records</code> - rDNS 本地记录</li>
        <li><code>GET /api/mail/records</code> - 邮局DNS所有记录</li>
        <li><code>GET /api/mail/stats</code> - 邮局DNS统计</li>
        <li><code>POST /api/mail/mx</code> - 添加/删除 MX 记录</li>
        <li><code>POST /api/mail/a</code> - 添加/删除 A 记录</li>
        <li><code>GET /api/upstreams?group=dn42</code> - 上游服务器状态</li>
        <li><code>GET /api/ha/status</code> - 主从状态</li>
    </ul>
</body>
</html>`)
}

// handleHealth 健康检查
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(HealthResponse{Status: "ok"})
}

// handleStats 统计信息
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	resp := StatsResponse{
		Mode: s.haMode,
	}

	if s.cache != nil {
		resp.Cache = s.cache.GetStats()
		resp.CacheSize = s.cache.GetSize()
	}

	if s.rdns != nil {
		resp.Rdns = s.rdns.GetStats()
	}

	if s.resolver != nil {
		resp.Upstreams = s.resolver.GetGroupNames()
	}

	json.NewEncoder(w).Encode(resp)
}

// handleHAStatus HA 状态
func (s *Server) handleHAStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	s.mu.Lock()
	status := s.haStatus
	s.mu.Unlock()

	mode := s.haMode
	if mode == "" {
		mode = "standalone"
	}

	if status == nil {
		status = map[string]string{"mode": mode}
	}

	json.NewEncoder(w).Encode(status)
}

// handleCacheFlush 清空缓存
func (s *Server) handleCacheFlush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.cache != nil {
		s.cache.Flush()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "cache flushed"})
}

// handleCacheStats 缓存统计
func (s *Server) handleCacheStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.cache == nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "cache disabled"})
		return
	}

	stats := s.cache.GetStats()
	size := s.cache.GetSize()

	hitRate := float64(0)
	if stats.TotalQueries > 0 {
		hitRate = float64(stats.CacheHits) / float64(stats.TotalQueries) * 100
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"size":       size,
		"total":      stats.TotalQueries,
		"hits":       stats.CacheHits,
		"misses":     stats.CacheMisses,
		"hit_rate":   fmt.Sprintf("%.1f%%", hitRate),
	})
}

// handleRdnsRecords rDNS 本地记录
func (s *Server) handleRdnsRecords(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.rdns == nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "rdns disabled"})
		return
	}

	// GET: 获取记录
	if r.Method == http.MethodGet {
		json.NewEncoder(w).Encode(RdnsRecordsResponse{
			Records: s.rdns.GetLocalRecords(),
		})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// handleUpstreams 上游服务器状态
func (s *Server) handleUpstreams(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.resolver == nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "resolver not available"})
		return
	}

	group := r.URL.Query().Get("group")
	if group == "" {
		group = "default"
	}

	stats := s.resolver.GetGroupStats(group)
	if stats == nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "group not found"})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"group":    group,
		"servers":  stats,
	})
}

// handleMailRecords 邮局DNS所有记录
func (s *Server) handleMailRecords(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.mail == nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "mail dns disabled"})
		return
	}

	if r.Method == http.MethodGet {
		records := s.mail.GetAllRecords()
		json.NewEncoder(w).Encode(records)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// handleMailStats 邮局DNS统计
func (s *Server) handleMailStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.mail == nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "mail dns disabled"})
		return
	}

	stats := s.mail.GetStats()
	json.NewEncoder(w).Encode(stats)
}

// handleMailMX MX记录管理
func (s *Server) handleMailMX(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.mail == nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "mail dns disabled"})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Action   string `json:"action"`   // add / delete
			Domain   string `json:"domain"`
			Server   string `json:"server"`
			Priority uint16 `json:"priority"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		switch req.Action {
		case "add":
			s.mail.AddMXRecord(req.Domain, req.Server, req.Priority)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		case "delete":
			s.mail.DeleteMXRecord(req.Domain, req.Server)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		default:
			http.Error(w, "invalid action", http.StatusBadRequest)
		}
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// handleMailA A记录管理
func (s *Server) handleMailA(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.mail == nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "mail dns disabled"})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Action string `json:"action"` // add / delete
			Domain string `json:"domain"`
			IP     string `json:"ip"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		switch req.Action {
		case "add":
			s.mail.AddARecord(req.Domain, req.IP)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		case "delete":
			s.mail.DeleteARecord(req.Domain)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		default:
			http.Error(w, "invalid action", http.StatusBadRequest)
		}
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// handleMailSPF SPF记录管理
func (s *Server) handleMailSPF(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.mail == nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "mail dns disabled"})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Action string `json:"action"`
			Domain string `json:"domain"`
			Value  string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		if req.Action == "add" {
			s.mail.AddSPF(req.Domain, req.Value)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		} else {
			http.Error(w, "invalid action", http.StatusBadRequest)
		}
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// handleMailDKIM DKIM记录管理
func (s *Server) handleMailDKIM(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.mail == nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "mail dns disabled"})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Action   string `json:"action"`
			Selector string `json:"selector"`
			Domain   string `json:"domain"`
			Value    string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		if req.Action == "add" {
			s.mail.AddDKIM(req.Selector, req.Domain, req.Value)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		} else {
			http.Error(w, "invalid action", http.StatusBadRequest)
		}
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// handleMailDMARC DMARC记录管理
func (s *Server) handleMailDMARC(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.mail == nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "mail dns disabled"})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Action string `json:"action"`
			Domain string `json:"domain"`
			Value  string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		if req.Action == "add" {
			s.mail.AddDMARC(req.Domain, req.Value)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		} else {
			http.Error(w, "invalid action", http.StatusBadRequest)
		}
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}
