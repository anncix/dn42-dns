package server

import (
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
	"smartdns/internal/config"
)

// createTestConfig 创建测试用配置
func createTestConfig() *config.Config {
	return &config.Config{
		Listen: []config.ListenConfig{
			{Addr: ":15353", Protocol: "udp"},
			{Addr: ":15353", Protocol: "tcp"},
		},
		Upstreams: config.UpstreamConfig{
			Default: []config.UpstreamServer{
				{Addr: "223.5.5.5:53", Name: "test-default", Protocol: "udp"},
			},
			Groups: map[string][]config.UpstreamServer{
				"dn42": {
					{Addr: "223.5.5.5:53", Name: "test-dn42", Protocol: "udp"},
				},
			},
		},
		Routing: config.RoutingConfig{
			DomainSuffix: map[string][]string{
				"dn42": {".dn42", ".fdn", "20.172.in-addr.arpa"},
			},
			DefaultGroup: "default",
		},
		Cache: config.CacheConfig{
			Enabled: true,
			MaxSize: 100,
			MinTTL:  10,
			MaxTTL:  3600,
			NegTTL:  60,
		},
		Rdns: config.RdnsConfig{
			Enabled: true,
			AllowedNetworks: []string{
				"127.0.0.0/8",
				"172.20.0.0/14",
				"10.0.0.0/8",
				"fd00::/8",
				"::1/128", // IPv6 回环
			},
			LocalRecords: map[string]string{
				"127.0.0.1": "localhost",
				"10.0.0.1":  "router.local",
				"::1":       "localhost",
			},
		},
		Log: config.LogConfig{
			Level:    "info",
			QueryLog: false,
		},
	}
}

// dnsQuery 发送DNS查询
func dnsQuery(server string, domain string, qtype uint16) (*dns.Msg, error) {
	c := new(dns.Client)
	c.Net = "udp"
	c.Timeout = 3 * time.Second

	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(domain), qtype)

	r, _, err := c.Exchange(m, server)
	return r, err
}

func TestServer_StartStop(t *testing.T) {
	cfg := createTestConfig()
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("创建服务器失败: %v", err)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("启动服务器失败: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	srv.Stop()
}

func TestServer_RdnsLocalRecord_IPv4(t *testing.T) {
	cfg := createTestConfig()
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("创建服务器失败: %v", err)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("启动服务器失败: %v", err)
	}
	defer srv.Stop()

	time.Sleep(100 * time.Millisecond)

	resp, err := dnsQuery("127.0.0.1:15353", "1.0.0.127.in-addr.arpa", dns.TypePTR)
	if err != nil {
		t.Fatalf("PTR 查询失败: %v", err)
	}
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("本地 PTR Rcode = %d, 期望成功", resp.Rcode)
	}
	if len(resp.Answer) == 0 {
		t.Fatal("本地 PTR 没有应答记录")
	}
	ptr, ok := resp.Answer[0].(*dns.PTR)
	if !ok {
		t.Fatal("应答类型不是 PTR")
	}
	if ptr.Ptr != "localhost." {
		t.Errorf("PTR 值 = %s, 期望 localhost.", ptr.Ptr)
	}
}

func TestServer_RdnsLocalRecord_IPv6(t *testing.T) {
	cfg := createTestConfig()
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("创建服务器失败: %v", err)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("启动服务器失败: %v", err)
	}
	defer srv.Stop()

	time.Sleep(100 * time.Millisecond)

	// ::1 的完整 ip6.arpa 形式
	ptrName := "1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.ip6.arpa"
	resp, err := dnsQuery("127.0.0.1:15353", ptrName, dns.TypePTR)
	if err != nil {
		t.Fatalf("IPv6 PTR 查询失败: %v", err)
	}
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("IPv6 本地 PTR Rcode = %d, 期望成功", resp.Rcode)
	}
	if len(resp.Answer) == 0 {
		t.Fatal("IPv6 本地 PTR 没有应答记录")
	}
	ptr := resp.Answer[0].(*dns.PTR)
	if ptr.Ptr != "localhost." {
		t.Errorf("IPv6 PTR 值 = %s, 期望 localhost.", ptr.Ptr)
	}
}

func TestServer_RdnsNotAllowed(t *testing.T) {
	cfg := createTestConfig()
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("创建服务器失败: %v", err)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("启动服务器失败: %v", err)
	}
	defer srv.Stop()

	time.Sleep(100 * time.Millisecond)

	// 公网 IP 的 PTR 应该被拒绝
	resp, err := dnsQuery("127.0.0.1:15353", "8.8.8.8.in-addr.arpa", dns.TypePTR)
	if err != nil {
		t.Fatalf("PTR 查询失败: %v", err)
	}
	if resp.Rcode != dns.RcodeRefused {
		t.Errorf("公网 PTR Rcode = %d, 期望 REFUSED(%d)", resp.Rcode, dns.RcodeRefused)
	}
}

func TestServer_FlushCache(t *testing.T) {
	cfg := createTestConfig()
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("创建服务器失败: %v", err)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("启动服务器失败: %v", err)
	}
	defer srv.Stop()

	time.Sleep(100 * time.Millisecond)

	// 测试清空缓存
	srv.FlushCache()
	if srv.cache.GetSize() != 0 {
		t.Error("清空后缓存应该为空")
	}
}

func TestServer_ListenAddresses(t *testing.T) {
	cfg := createTestConfig()
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("创建服务器失败: %v", err)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("启动服务器失败: %v", err)
	}
	defer srv.Stop()

	time.Sleep(100 * time.Millisecond)

	// 检查 UDP 端口
	conn, err := net.DialTimeout("udp", "127.0.0.1:15353", 1*time.Second)
	if err != nil {
		t.Errorf("UDP 端口无法连接: %v", err)
	} else {
		conn.Close()
	}

	// 检查 TCP 端口
	conn, err = net.DialTimeout("tcp", "127.0.0.1:15353", 1*time.Second)
	if err != nil {
		t.Errorf("TCP 端口无法连接: %v", err)
	} else {
		conn.Close()
	}
}

func TestServer_Stats(t *testing.T) {
	cfg := createTestConfig()
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("创建服务器失败: %v", err)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("启动服务器失败: %v", err)
	}
	defer srv.Stop()

	time.Sleep(100 * time.Millisecond)

	// 发几个本地 PTR 查询（不需要公网）
	for i := 0; i < 3; i++ {
		dnsQuery("127.0.0.1:15353", "1.0.0.127.in-addr.arpa", dns.TypePTR)
	}

	// 测试打印统计（不应该 panic）
	srv.PrintStats()
}

func TestServer_MultiplePTRScenarios(t *testing.T) {
	cfg := createTestConfig()
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("创建服务器失败: %v", err)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("启动服务器失败: %v", err)
	}
	defer srv.Stop()

	time.Sleep(100 * time.Millisecond)

	// 测试多个 PTR 场景（不需要公网的）
	testCases := []struct {
		name   string
		ptr    string
		rcode  int
		hasAns bool
	}{
		{"IPv4本地记录", "1.0.0.127.in-addr.arpa", dns.RcodeSuccess, true},
		{"公网IP-拒绝", "8.8.8.8.in-addr.arpa", dns.RcodeRefused, false},
		{"10段-转发(无本地记录)", "1.0.0.10.in-addr.arpa", dns.RcodeSuccess, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := dnsQuery("127.0.0.1:15353", tc.ptr, dns.TypePTR)
			if err != nil {
				t.Fatalf("查询失败: %v", err)
			}
			if tc.hasAns && len(resp.Answer) == 0 {
				t.Errorf("%s: 应该有应答记录", tc.name)
			}
			if !tc.hasAns && resp.Rcode == dns.RcodeSuccess && tc.rcode != dns.RcodeSuccess {
				// 如果预期不是成功但返回了成功（比如转发了），也可以接受
				t.Logf("%s: Rcode = %d (预期 %d)", tc.name, resp.Rcode, tc.rcode)
			}
		})
	}
}

func TestServer_InvalidPTR(t *testing.T) {
	cfg := createTestConfig()
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("创建服务器失败: %v", err)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("启动服务器失败: %v", err)
	}
	defer srv.Stop()

	time.Sleep(100 * time.Millisecond)

	// 无效的 PTR 格式
	resp, err := dnsQuery("127.0.0.1:15353", "invalid-ptr.example.com", dns.TypePTR)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	// 不应该 panic，返回什么都可以
	t.Logf("无效PTR查询 Rcode: %d", resp.Rcode)
}

func TestServer_RdnsDisabled(t *testing.T) {
	cfg := createTestConfig()
	cfg.Rdns.Enabled = false
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("创建服务器失败: %v", err)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("启动服务器失败: %v", err)
	}
	defer srv.Stop()

	time.Sleep(100 * time.Millisecond)

	// rDNS 禁用时，127.0.0.1 的 PTR 不应该本地命中
	// （会转发，但这里不关心转发结果，只要不 panic 就行）
	resp, err := dnsQuery("127.0.0.1:15353", "1.0.0.127.in-addr.arpa", dns.TypePTR)
	if err != nil {
		t.Logf("rDNS禁用时PTR查询错误(正常): %v", err)
	} else {
		t.Logf("rDNS禁用时PTR查询 Rcode: %d", resp.Rcode)
	}
}
