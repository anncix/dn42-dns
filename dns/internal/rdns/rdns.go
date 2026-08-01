package rdns

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/miekg/dns"
	"smartdns/internal/config"
)

// Handler rDNS 处理器
type Handler struct {
	cfg             *config.RdnsConfig
	allowedNetworks []*net.IPNet
	localRecords    map[string]string
	stats           Stats
	mu              sync.RWMutex

	// 网段匹配结果缓存（减少重复计算）
	allowCache      map[string]bool
	allowCacheHits  uint64
	allowCacheMax   int // 缓存最大条目数，超过就清空重建
}

// Stats rDNS 统计
type Stats struct {
	TotalQueries uint64
	LocalHits    uint64
	Forwarded    uint64
	Dropped      uint64
}

// NewHandler 创建 rDNS 处理器
func NewHandler(cfg *config.RdnsConfig) (*Handler, error) {
	h := &Handler{
		cfg:          cfg,
		localRecords: make(map[string]string),
		allowCache:   make(map[string]bool),
		allowCacheMax: 1024,
	}

	if !cfg.Enabled {
		return h, nil
	}

	// 解析允许的网段
	for _, cidr := range cfg.AllowedNetworks {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			fmt.Printf("警告: 无效的网段 %s: %v\n", cidr, err)
			continue
		}
		h.allowedNetworks = append(h.allowedNetworks, network)
	}

	// 加载本地记录
	for ip, domain := range cfg.LocalRecords {
		if net.ParseIP(ip) != nil {
			h.localRecords[ip] = domain
		}
	}

	return h, nil
}

// HandlePTR 处理 PTR 查询
// 返回: 响应消息（如果本地有记录）、是否应该转发、nil表示不处理
func (h *Handler) HandlePTR(r *dns.Msg, clientIP string) (*dns.Msg, bool) {
	if !h.cfg.Enabled || len(r.Question) == 0 {
		return nil, true
	}

	q := r.Question[0]
	if q.Qtype != dns.TypePTR {
		return nil, true
	}

	statsAdd(&h.stats.TotalQueries, 1)

	// 从 PTR 域名解析 IP
	ip := ptrToIP(q.Name)
	if ip == nil {
		statsAdd(&h.stats.Dropped, 1)
		return nil, false // 无效的 PTR 格式，直接丢弃
	}

	// 检查是否在允许的网段内
	if !h.isAllowed(ip) {
		statsAdd(&h.stats.Dropped, 1)
		return nil, false // 不在允许网段，丢弃
	}

	// 查找本地记录
	ipStr := ip.String()
	if domain, ok := h.localRecords[ipStr]; ok {
		statsAdd(&h.stats.LocalHits, 1)
		return h.buildResponse(r, q.Name, domain), false
	}

	// 本地没有记录，转发到上游
	statsAdd(&h.stats.Forwarded, 1)
	return nil, true
}

// isAllowed 检查 IP 是否在允许的网段内（带缓存）
func (h *Handler) isAllowed(ip net.IP) bool {
	ipStr := ip.String()

	// 先查缓存（读锁）
	h.mu.RLock()
	if result, ok := h.allowCache[ipStr]; ok {
		h.mu.RUnlock()
		return result
	}
	h.mu.RUnlock()

	// 缓存未命中，计算结果
	result := false
	for _, network := range h.allowedNetworks {
		if network.Contains(ip) {
			result = true
			break
		}
	}

	// 写入缓存（写锁）
	h.mu.Lock()
	// 缓存满了就清空（简单策略，因为缓存量不大）
	if len(h.allowCache) >= h.allowCacheMax {
		h.allowCache = make(map[string]bool)
	}
	h.allowCache[ipStr] = result
	h.mu.Unlock()

	return result
}

// buildResponse 构建 PTR 响应
func (h *Handler) buildResponse(r *dns.Msg, qName, domain string) *dns.Msg {
	resp := new(dns.Msg)
	resp.SetReply(r)
	resp.Authoritative = true

	ptr := &dns.PTR{
		Hdr: dns.RR_Header{
			Name:   qName,
			Rrtype: dns.TypePTR,
			Class:  dns.ClassINET,
			Ttl:    3600,
		},
		Ptr: dns.Fqdn(domain),
	}
	resp.Answer = append(resp.Answer, ptr)
	return resp
}

// GetStats 获取统计
func (h *Handler) GetStats() Stats {
	return Stats{
		TotalQueries: atomic.LoadUint64(&h.stats.TotalQueries),
		LocalHits:    atomic.LoadUint64(&h.stats.LocalHits),
		Forwarded:    atomic.LoadUint64(&h.stats.Forwarded),
		Dropped:      atomic.LoadUint64(&h.stats.Dropped),
	}
}

// StatsSummary 获取统计摘要
func (h *Handler) StatsSummary() string {
	s := h.GetStats()
	return fmt.Sprintf("rDNS: 总查询%d, 本地命中%d, 转发%d, 丢弃%d",
		s.TotalQueries, s.LocalHits, s.Forwarded, s.Dropped)
}

// GetLocalRecords 获取本地 rDNS 记录（返回副本）
func (h *Handler) GetLocalRecords() map[string]string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	records := make(map[string]string, len(h.localRecords))
	for ip, domain := range h.localRecords {
		records[ip] = domain
	}
	return records
}

// SetLocalRecords 设置本地 rDNS 记录（全量替换）
func (h *Handler) SetLocalRecords(records map[string]string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.localRecords = make(map[string]string, len(records))
	for ip, domain := range records {
		h.localRecords[ip] = domain
	}
}

// === 辅助函数 ===

// ptrToIP 从 PTR 域名解析 IP 地址
func ptrToIP(ptrName string) net.IP {
	name := strings.TrimSuffix(strings.ToLower(ptrName), ".")

	// IPv4: x.x.x.x.in-addr.arpa
	if strings.HasSuffix(name, ".in-addr.arpa") {
		ipStr := strings.TrimSuffix(name, ".in-addr.arpa")
		parts := strings.Split(ipStr, ".")
		if len(parts) != 4 {
			return nil
		}
		// 反序构建 IP，避免 fmt.Sprintf 分配
		ip := make(net.IP, 4)
		for i := 0; i < 4; i++ {
			n, err := atoi(parts[3-i])
			if err != nil || n > 255 {
				return nil
			}
			ip[i] = byte(n)
		}
		return ip
	}

	// IPv6: x.ip6.arpa
	if strings.HasSuffix(name, ".ip6.arpa") {
		ipStr := strings.TrimSuffix(name, ".ip6.arpa")
		parts := strings.Split(ipStr, ".")
		if len(parts) != 32 {
			return nil
		}
		// 直接构建 IPv6 字节数组，避免多次字符串分配
		ip := make(net.IP, 16)
		for i := 0; i < 32; i++ {
			// parts[0] 是最高位 nibble，parts[31] 是最低位
			// 反转：parts[31-i] 是第 i 个 nibble
			nibble := parts[31-i]
			if len(nibble) != 1 {
				return nil
			}
			val, ok := hexToByte(nibble[0])
			if !ok {
				return nil
			}
			byteIdx := i / 2
			if i%2 == 0 {
				ip[byteIdx] = val << 4
			} else {
				ip[byteIdx] |= val
			}
		}
		return ip
	}

	return nil
}

// 原子操作封装
func statsAdd(p *uint64, v uint64) {
	atomic.AddUint64(p, v)
}

func statsGet(p *uint64) uint64 {
	return atomic.LoadUint64(p)
}

// hexToByte 十六进制字符转字节值（0-15）
func hexToByte(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}

// atoi 简单的字符串转整数（只用于0-255范围，比strconv快）
func atoi(s string) (int, error) {
	if len(s) == 0 || len(s) > 3 {
		return 0, fmt.Errorf("invalid length")
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid char")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
