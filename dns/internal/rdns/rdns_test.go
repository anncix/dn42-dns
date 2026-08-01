package rdns

import (
	"net"
	"testing"

	"github.com/miekg/dns"
	"smartdns/internal/config"
)

func TestPtrToIP_IPv4(t *testing.T) {
	tests := []struct {
		ptrName string
		wantIP  string
	}{
		{"1.0.0.127.in-addr.arpa.", "127.0.0.1"},
		{"53.0.20.172.in-addr.arpa.", "172.20.0.53"},
		{"1.1.168.192.in-addr.arpa.", "192.168.1.1"},
	}

	for _, tt := range tests {
		ip := ptrToIP(tt.ptrName)
		if ip == nil {
			t.Errorf("ptrToIP(%s) 返回 nil", tt.ptrName)
			continue
		}
		if ip.String() != tt.wantIP {
			t.Errorf("ptrToIP(%s) = %s, 期望 %s", tt.ptrName, ip.String(), tt.wantIP)
		}
	}
}

func TestPtrToIP_IPv6(t *testing.T) {
	// ::1 的完整 ip6.arpa 形式
	ptrName := "1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.ip6.arpa."
	ip := ptrToIP(ptrName)
	if ip == nil {
		t.Fatal("ptrToIP(::1) 返回 nil")
	}
	if ip.String() != "::1" {
		t.Errorf("ptrToIP(::1) = %s, 期望 ::1", ip.String())
	}
}

func TestPtrToIP_Invalid(t *testing.T) {
	tests := []string{
		"",
		"example.com.",
		"1.2.3.in-addr.arpa.", // 只有3段
	}

	for _, ptrName := range tests {
		ip := ptrToIP(ptrName)
		if ip != nil {
			t.Errorf("ptrToIP(%s) 应该返回 nil, 实际 %s", ptrName, ip.String())
		}
	}
}

func TestNewHandler(t *testing.T) {
	cfg := &config.RdnsConfig{
		Enabled: true,
		AllowedNetworks: []string{
			"172.20.0.0/14",
			"fd00::/8",
			"127.0.0.0/8",
			"invalid-cidr", // 无效的，应该被跳过
		},
		LocalRecords: map[string]string{
			"127.0.0.1": "localhost",
			"::1":       "localhost",
		},
	}

	h, err := NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler 失败: %v", err)
	}

	if len(h.allowedNetworks) != 3 {
		t.Errorf("允许网段数量 = %d, 期望 3", len(h.allowedNetworks))
	}

	if len(h.localRecords) != 2 {
		t.Errorf("本地记录数量 = %d, 期望 2", len(h.localRecords))
	}
}

func TestIsAllowed(t *testing.T) {
	cfg := &config.RdnsConfig{
		Enabled: true,
		AllowedNetworks: []string{
			"172.20.0.0/14",
			"fd00::/8",
			"127.0.0.0/8",
		},
	}

	h, _ := NewHandler(cfg)

	tests := []struct {
		ip    string
		allow bool
	}{
		{"127.0.0.1", true},
		{"172.20.0.53", true},
		{"172.22.123.45", true},
		{"172.23.255.255", true},
		{"172.24.0.0", false}, // 超出/14范围
		{"8.8.8.8", false},
		{"fd00::1", true},
		{"2001:db8::1", false},
	}

	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		result := h.isAllowed(ip)
		if result != tt.allow {
			t.Errorf("isAllowed(%s) = %v, 期望 %v", tt.ip, result, tt.allow)
		}
	}
}

func TestHandlePTR_LocalRecord(t *testing.T) {
	cfg := &config.RdnsConfig{
		Enabled: true,
		AllowedNetworks: []string{
			"127.0.0.0/8",
		},
		LocalRecords: map[string]string{
			"127.0.0.1": "localhost",
		},
	}

	h, _ := NewHandler(cfg)

	r := new(dns.Msg)
	r.SetQuestion("1.0.0.127.in-addr.arpa.", dns.TypePTR)

	resp, shouldForward := h.HandlePTR(r, "127.0.0.1")

	if resp == nil {
		t.Fatal("本地PTR查询应该返回响应")
	}
	if shouldForward {
		t.Error("本地PTR查询不应该转发")
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("应答记录数 = %d, 期望 1", len(resp.Answer))
	}

	ptr, ok := resp.Answer[0].(*dns.PTR)
	if !ok {
		t.Fatal("应答类型不是PTR")
	}
	if ptr.Ptr != "localhost." {
		t.Errorf("PTR值 = %s, 期望 localhost.", ptr.Ptr)
	}
}

func TestHandlePTR_NotAllowed(t *testing.T) {
	cfg := &config.RdnsConfig{
		Enabled: true,
		AllowedNetworks: []string{
			"127.0.0.0/8",
		},
		LocalRecords: map[string]string{},
	}

	h, _ := NewHandler(cfg)

	r := new(dns.Msg)
	r.SetQuestion("8.8.8.8.in-addr.arpa.", dns.TypePTR)

	resp, shouldForward := h.HandlePTR(r, "127.0.0.1")

	if resp != nil {
		t.Error("不在允许网段的PTR查询不应该返回响应")
	}
	if shouldForward {
		t.Error("不在允许网段的PTR查询不应该转发")
	}
}

func TestHandlePTR_Disabled(t *testing.T) {
	cfg := &config.RdnsConfig{
		Enabled: false,
	}

	h, _ := NewHandler(cfg)

	r := new(dns.Msg)
	r.SetQuestion("1.0.0.127.in-addr.arpa.", dns.TypePTR)

	_, shouldForward := h.HandlePTR(r, "127.0.0.1")
	if !shouldForward {
		t.Error("rDNS禁用时应该转发所有PTR查询")
	}
}

func TestStats(t *testing.T) {
	cfg := &config.RdnsConfig{
		Enabled: true,
		AllowedNetworks: []string{
			"127.0.0.0/8",
			"10.0.0.0/8",
		},
		LocalRecords: map[string]string{
			"127.0.0.1": "localhost",
		},
	}

	h, _ := NewHandler(cfg)

	// 本地命中
	r1 := new(dns.Msg)
	r1.SetQuestion("1.0.0.127.in-addr.arpa.", dns.TypePTR)
	h.HandlePTR(r1, "127.0.0.1")

	// 转发
	r2 := new(dns.Msg)
	r2.SetQuestion("1.0.0.10.in-addr.arpa.", dns.TypePTR)
	h.HandlePTR(r2, "127.0.0.1")

	// 丢弃
	r3 := new(dns.Msg)
	r3.SetQuestion("8.8.8.8.in-addr.arpa.", dns.TypePTR)
	h.HandlePTR(r3, "127.0.0.1")

	stats := h.GetStats()
	if stats.TotalQueries != 3 {
		t.Errorf("总查询数 = %d, 期望 3", stats.TotalQueries)
	}
	if stats.LocalHits != 1 {
		t.Errorf("本地命中数 = %d, 期望 1", stats.LocalHits)
	}
	if stats.Forwarded != 1 {
		t.Errorf("转发数 = %d, 期望 1", stats.Forwarded)
	}
	if stats.Dropped != 1 {
		t.Errorf("丢弃数 = %d, 期望 1", stats.Dropped)
	}
}
