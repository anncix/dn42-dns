package router

import (
	"testing"

	"smartdns/internal/config"
)

func TestRoute_SuffixMatch(t *testing.T) {
	cfg := &config.RoutingConfig{
		DomainSuffix: map[string][]string{
			"dn42": {".dn42", ".fdn"},
			"home": {".home.arpa"},
		},
		DefaultGroup: "default",
	}

	r := NewRouter(cfg)

	tests := []struct {
		domain string
		group  string
	}{
		// dn42 后缀
		{"test.dn42.", "dn42"},
		{"sub.test.dn42.", "dn42"},
		{"DN42.", "dn42"}, // 大小写不敏感
		{"example.fdn.", "dn42"},

		// home 后缀
		{"router.home.arpa.", "home"},

		// 公网域名
		{"google.com.", "default"},
		{"github.com.", "default"},
	}

	for _, tt := range tests {
		result := r.Route(tt.domain)
		if result != tt.group {
			t.Errorf("Route(%s) = %s, 期望 %s", tt.domain, result, tt.group)
		}
	}
}

func TestRoute_LongestMatch(t *testing.T) {
	cfg := &config.RoutingConfig{
		DomainSuffix: map[string][]string{
			"group1": {".com"},
			"group2": {"example.com"},
		},
		DefaultGroup: "default",
	}

	r := NewRouter(cfg)

	// example.com 应该走 group2（更长的匹配）
	result := r.Route("example.com.")
	if result != "group2" {
		t.Errorf("最长匹配失败: Route(example.com) = %s, 期望 group2", result)
	}

	// other.com 应该走 group1
	result = r.Route("other.com.")
	if result != "group1" {
		t.Errorf("Route(other.com) = %s, 期望 group1", result)
	}
}

func TestStatsSummary(t *testing.T) {
	cfg := &config.RoutingConfig{
		DomainSuffix: map[string][]string{
			"dn42": {".dn42"},
		},
		DefaultGroup: "default",
	}

	r := NewRouter(cfg)
	r.Route("test.dn42.")
	r.Route("google.com.")

	summary := r.StatsSummary()
	if summary == "" {
		t.Error("StatsSummary 不应该为空")
	}
}
