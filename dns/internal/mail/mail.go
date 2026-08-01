package mail

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/miekg/dns"
	"smartdns/internal/config"
)

// Handler 邮局DNS处理器
type Handler struct {
	cfg   *config.MailConfig
	stats Stats
	mu    sync.RWMutex

	// 按域名索引的 MX 记录（域名 -> []MXRecord）
	mxIndex map[string][]*config.MXRecord

	// 动态添加的记录（运行时添加）
	runtimeMX    map[string][]*config.MXRecord
	runtimeA      map[string]string
	runtimeSPF   map[string]string
	runtimeDKIM  map[string]string
	runtimeDMARC map[string]string
}

// Stats 统计
type Stats struct {
	TotalQueries uint64
	LocalHits    uint64
	Forwarded    uint64
	MXQueries    uint64
	TXTQueries   uint64
	AQueries     uint64
}

// NewHandler 创建邮局DNS处理器
func NewHandler(cfg *config.MailConfig) (*Handler, error) {
	h := &Handler{
		cfg:          cfg,
		mxIndex:      make(map[string][]*config.MXRecord),
		runtimeMX:    make(map[string][]*config.MXRecord),
		runtimeA:      make(map[string]string),
		runtimeSPF:   make(map[string]string),
		runtimeDKIM:  make(map[string]string),
		runtimeDMARC: make(map[string]string),
	}

	if !cfg.Enabled {
		return h, nil
	}

	// 构建 MX 索引
	for i := range cfg.MX {
		domain := normalizeDomain(cfg.MX[i].Domain)
		h.mxIndex[domain] = append(h.mxIndex[domain], &cfg.MX[i])
	}

	return h, nil
}

// HandleQuery 处理邮件相关DNS查询
// 返回 (响应消息, 是否本地处理)
func (h *Handler) HandleQuery(r *dns.Msg) (*dns.Msg, bool) {
	if !h.cfg.Enabled || r == nil || len(r.Question) == 0 {
		return nil, false
	}

	q := r.Question[0]
	qname := strings.ToLower(strings.TrimSuffix(q.Name, "."))

	atomic.AddUint64(&h.stats.TotalQueries, 1)

	var resp *dns.Msg
	handled := false

	switch q.Qtype {
	case dns.TypeMX:
		atomic.AddUint64(&h.stats.MXQueries, 1)
		resp = h.handleMX(r, qname)
	case dns.TypeTXT:
		atomic.AddUint64(&h.stats.TXTQueries, 1)
		resp = h.handleTXT(r, qname)
	case dns.TypeA:
		atomic.AddUint64(&h.stats.AQueries, 1)
		resp = h.handleA(r, qname)
	default:
		return nil, false
	}

	if resp != nil {
		atomic.AddUint64(&h.stats.LocalHits, 1)
		handled = true
	}

	return resp, handled
}

// handleMX 处理 MX 查询
func (h *Handler) handleMX(r *dns.Msg, qname string) *dns.Msg {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// 查找 MX 记录
	mxRecords := h.findMXRecords(qname)
	if len(mxRecords) == 0 {
		return nil
	}

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	ttl := h.cfg.TTL

	for _, mx := range mxRecords {
		rr := &dns.MX{
			Hdr:      dns.RR_Header{
				Name:   dns.Fqdn(qname),
				Rrtype: dns.TypeMX,
				Class:  dns.ClassINET,
				Ttl:    ttl,
			},
			Preference: mx.Priority,
			Mx:         dns.Fqdn(mx.Server),
		}
		m.Answer = append(m.Answer, rr)

		// 附加 A 记录（如果有）
		serverName := normalizeDomain(mx.Server)
		if ip := h.findARecord(serverName); ip != "" {
			aRR := &dns.A{
				Hdr: dns.RR_Header{
					Name:   dns.Fqdn(mx.Server),
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    ttl,
				},
				A: net.ParseIP(ip),
			}
			m.Extra = append(m.Extra, aRR)
		}
	}

	return m
}

// handleTXT 处理 TXT 查询（SPF/DKIM/DMARC）
func (h *Handler) handleTXT(r *dns.Msg, qname string) *dns.Msg {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var txtValue = ""
	var found bool

	// 检查是否是 SPF 查询（域名本身的 TXT，以 v=spf1 开头）
	if val := h.findSPF(qname); val != "" {
		txtValue = val
		found = true
	}

	// 检查是否是 DKIM 查询（selector._domainkey.example.com）
	if !found {
		if val := h.findDKIM(qname); val != "" {
			txtValue = val
			found = true
		}
	}

	// 检查是否是 DMARC 查询（_dmarc.example.com）
	if !found {
		if val := h.findDMARC(qname); val != "" {
			txtValue = val
			found = true
		}
	}

	if !found {
		return nil
	}

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	rr := &dns.TXT{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(qname),
			Rrtype: dns.TypeTXT,
			Class:  dns.ClassINET,
			Ttl:    h.cfg.TTL,
		},
		Txt: []string{txtValue},
	}
	m.Answer = append(m.Answer, rr)

	return m
}

// handleA 处理邮件服务器 A 记录查询
func (h *Handler) handleA(r *dns.Msg, qname string) *dns.Msg {
	h.mu.RLock()
	defer h.mu.RUnlock()

	ip := h.findARecord(qname)
	if ip == "" {
		return nil
	}

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	rr := &dns.A{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(qname),
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    h.cfg.TTL,
		},
		A: net.ParseIP(ip),
	}
	m.Answer = append(m.Answer, rr)

	return m
}

// findMXRecords 查找 MX 记录
func (h *Handler) findMXRecords(domain string) []*config.MXRecord {
	domain = normalizeDomain(domain)

	// 先查运行时
	if records, ok := h.runtimeMX[domain]; ok && len(records) > 0 {
		return records
	}

	// 再查配置
	if records, ok := h.mxIndex[domain]; ok {
		return records
	}

	return nil
}

// findARecord 查找 A 记录
func (h *Handler) findARecord(domain string) string {
	domain = normalizeDomain(domain)

	// 先查运行时
	if ip, ok := h.runtimeA[domain]; ok && ip != "" {
		return ip
	}

	// 再查配置
	if ip, ok := h.cfg.A[domain]; ok {
		return ip
	}

	return ""
}

// findSPF 查找 SPF 记录
func (h *Handler) findSPF(domain string) string {
	domain = normalizeDomain(domain)

	if val, ok := h.runtimeSPF[domain]; ok && val != "" {
		return val
	}

	if val, ok := h.cfg.SPF[domain]; ok {
		return val
	}

	return ""
}

// findDKIM 查找 DKIM 记录
func (h *Handler) findDKIM(qname string) string {
	// DKIM 格式: selector._domainkey.example.com
	qname = normalizeDomain(qname)

	if val, ok := h.runtimeDKIM[qname]; ok && val != "" {
		return val
	}

	if val, ok := h.cfg.DKIM[qname]; ok {
		return val
	}

	return ""
}

// findDMARC 查找 DMARC 记录
func (h *Handler) findDMARC(qname string) string {
	// DMARC 格式: _dmarc.example.com
	qname = normalizeDomain(qname)

	if val, ok := h.runtimeDMARC[qname]; ok && val != "" {
		return val
	}

	if val, ok := h.cfg.DMARC[qname]; ok {
		return val
	}

	return ""
}

// ============ 运行时记录管理 ============

// AddMXRecord 添加 MX 记录
func (h *Handler) AddMXRecord(domain, server string, priority uint16) {
	h.mu.Lock()
	defer h.mu.Unlock()

	domain = normalizeDomain(domain)
	rec := &config.MXRecord{
		Domain:   domain,
		Server:   server,
		Priority: priority,
	}
	h.runtimeMX[domain] = append(h.runtimeMX[domain], rec)
}

// AddARecord 添加 A 记录
func (h *Handler) AddARecord(domain, ip string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.runtimeA[normalizeDomain(domain)] = ip
}

// AddSPF 添加 SPF 记录
func (h *Handler) AddSPF(domain, value string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.runtimeSPF[normalizeDomain(domain)] = value
}

// AddDKIM 添加 DKIM 记录
func (h *Handler) AddDKIM(selector, domain, value string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := fmt.Sprintf("%s._domainkey.%s", selector, normalizeDomain(domain))
	h.runtimeDKIM[key] = value
}

// AddDMARC 添加 DMARC 记录
func (h *Handler) AddDMARC(domain, value string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := fmt.Sprintf("_dmarc.%s", normalizeDomain(domain))
	h.runtimeDMARC[key] = value
}

// DeleteMXRecord 删除 MX 记录
func (h *Handler) DeleteMXRecord(domain, server string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	domain = normalizeDomain(domain)
	records := h.runtimeMX[domain]
	for i, r := range records {
		if normalizeDomain(r.Server) == normalizeDomain(server) {
			h.runtimeMX[domain] = append(records[:i], records[i+1:]...)
			// 如果删完了，清理 key
			if len(h.runtimeMX[domain]) == 0 {
				delete(h.runtimeMX, domain)
			}
			return true
		}
	}
	return false
}

// DeleteARecord 删除 A 记录
func (h *Handler) DeleteARecord(domain string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	domain = normalizeDomain(domain)
	if _, ok := h.runtimeA[domain]; ok {
		delete(h.runtimeA, domain)
		return true
	}
	return false
}

// GetAllRecords 获取所有记录（用于 API 和 Web 面板）
func (h *Handler) GetAllRecords() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make(map[string]interface{})

	// MX 记录（配置 + 运行时合并）
	mxRecords := make(map[string][]*config.MXRecord)
	for k, v := range h.mxIndex {
		mxRecords[k] = v
	}
	for k, v := range h.runtimeMX {
		mxRecords[k] = append(mxRecords[k], v...)
	}
	result["mx"] = mxRecords

	// A 记录
	aRecords := make(map[string]string)
	for k, v := range h.cfg.A {
		aRecords[k] = v
	}
	for k, v := range h.runtimeA {
		aRecords[k] = v
	}
	result["a"] = aRecords

	// SPF
	spfRecords := make(map[string]string)
	for k, v := range h.cfg.SPF {
		spfRecords[k] = v
	}
	for k, v := range h.runtimeSPF {
		spfRecords[k] = v
	}
	result["spf"] = spfRecords

	// DKIM
	dkimRecords := make(map[string]string)
	for k, v := range h.cfg.DKIM {
		dkimRecords[k] = v
	}
	for k, v := range h.runtimeDKIM {
		dkimRecords[k] = v
	}
	result["dkim"] = dkimRecords

	// DMARC
	dmarcRecords := make(map[string]string)
	for k, v := range h.cfg.DMARC {
		dmarcRecords[k] = v
	}
	for k, v := range h.runtimeDMARC {
		dmarcRecords[k] = v
	}
	result["dmarc"] = dmarcRecords

	return result
}

// GetStats 获取统计
func (h *Handler) GetStats() Stats {
	return Stats{
		TotalQueries: atomic.LoadUint64(&h.stats.TotalQueries),
		LocalHits:    atomic.LoadUint64(&h.stats.LocalHits),
		Forwarded:    atomic.LoadUint64(&h.stats.Forwarded),
		MXQueries:    atomic.LoadUint64(&h.stats.MXQueries),
		TXTQueries:   atomic.LoadUint64(&h.stats.TXTQueries),
		AQueries:     atomic.LoadUint64(&h.stats.AQueries),
	}
}

// StatsSummary 统计摘要
func (h *Handler) StatsSummary() string {
	s := h.GetStats()
	return fmt.Sprintf("邮局DNS: 总查询%d, 本地命中%d, MX%d, TXT%d, A%d",
		s.TotalQueries, s.LocalHits, s.MXQueries, s.TXTQueries, s.AQueries)
}

// normalizeDomain 标准化域名
func normalizeDomain(d string) string {
	d = strings.ToLower(strings.TrimSpace(d))
	d = strings.TrimSuffix(d, ".")
	return d
}
