package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 主配置结构
type Config struct {
	Listen    []ListenConfig  `yaml:"listen"`
	Upstreams UpstreamConfig  `yaml:"upstreams"`
	Routing   RoutingConfig   `yaml:"routing"`
	AdBlock   AdBlockConfig   `yaml:"adblock"`
	Cache     CacheConfig     `yaml:"cache"`
	Rdns      RdnsConfig      `yaml:"rdns"`
	Log       LogConfig       `yaml:"log"`
}

// ListenConfig 监听配置
type ListenConfig struct {
	Addr      string `yaml:"addr"`
	Protocol  string `yaml:"protocol"` // udp / tcp / dot / doh
	CertFile  string `yaml:"cert_file,omitempty"`
	KeyFile   string `yaml:"key_file,omitempty"`
}

// UpstreamConfig 上游配置
type UpstreamConfig struct {
	Default []UpstreamServer      `yaml:"default"`
	Groups  map[string][]UpstreamServer `yaml:"groups"`
}

// UpstreamServer 上游服务器
type UpstreamServer struct {
	Addr     string `yaml:"addr"`
	Name     string `yaml:"name"`
	Protocol string `yaml:"protocol,omitempty"` // udp / tcp / dot / doh / doq
}

// RoutingConfig 分流配置
type RoutingConfig struct {
	DomainSuffix map[string][]string `yaml:"domain_suffix"`
	DomainExact  map[string][]string `yaml:"domain_exact"`
	DefaultGroup string              `yaml:"default_group"`
}

// AdBlockConfig 广告拦截配置
type AdBlockConfig struct {
	Enabled        bool     `yaml:"enabled"`
	BlockMode      string   `yaml:"block_mode"` // nxdomain / null / nodata
	BlockIPv4      string   `yaml:"block_ipv4"`
	BlockIPv6      string   `yaml:"block_ipv6"`
	BlacklistFiles []string `yaml:"blacklist_files"`
	WhitelistFiles []string `yaml:"whitelist_files"`
	CustomBlacklist []string `yaml:"custom_blacklist"`
	CustomWhitelist []string `yaml:"custom_whitelist"`
}

// CacheConfig 缓存配置
type CacheConfig struct {
	Enabled        bool   `yaml:"enabled"`
	MaxSize        int    `yaml:"max_size"`
	MinTTL         uint32 `yaml:"min_ttl"`
	MaxTTL         uint32 `yaml:"max_ttl"`
	NegTTL         uint32 `yaml:"neg_ttl"`
	LazyCache      bool   `yaml:"lazy_cache"`
	LazyCacheTTL   uint32 `yaml:"lazy_cache_ttl"`
	Prefetch       bool   `yaml:"prefetch"`
	PrefetchThreshold int `yaml:"prefetch_threshold"`
}

// RdnsConfig 反向DNS配置
type RdnsConfig struct {
	Enabled        bool              `yaml:"enabled"`
	AllowedNetworks []string         `yaml:"allowed_networks"`
	LocalRecords   map[string]string `yaml:"local_records"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level        string          `yaml:"level"`
	QueryLog     QueryLogConfig  `yaml:"query_log"`
	StatsInterval int             `yaml:"stats_interval"`
}

// QueryLogConfig 查询日志配置
type QueryLogConfig struct {
	Enabled     bool `yaml:"enabled"`
	OnlyBlocked bool `yaml:"only_blocked"`
}

// LoadConfig 从文件加载配置
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 设置默认值
	setDefaults(&cfg)

	return &cfg, nil
}

// setDefaults 设置默认值
func setDefaults(cfg *Config) {
	// 默认监听
	if len(cfg.Listen) == 0 {
		cfg.Listen = []ListenConfig{
			{Addr: ":53", Protocol: "udp"},
			{Addr: ":53", Protocol: "tcp"},
		}
	}

	// 默认上游
	if len(cfg.Upstreams.Default) == 0 {
		cfg.Upstreams.Default = []UpstreamServer{
			{Addr: "223.5.5.5:53", Name: "阿里DNS"},
			{Addr: "119.29.29.29:53", Name: "腾讯DNS"},
		}
	}

	// 默认分流组
	if cfg.Routing.DefaultGroup == "" {
		cfg.Routing.DefaultGroup = "default"
	}

	// 广告拦截默认值
	if cfg.AdBlock.BlockMode == "" {
		cfg.AdBlock.BlockMode = "nxdomain"
	}
	if cfg.AdBlock.BlockIPv4 == "" {
		cfg.AdBlock.BlockIPv4 = "0.0.0.0"
	}
	if cfg.AdBlock.BlockIPv6 == "" {
		cfg.AdBlock.BlockIPv6 = "::"
	}

	// 缓存默认值
	if cfg.Cache.MaxSize == 0 {
		cfg.Cache.MaxSize = 10000
	}
	if cfg.Cache.MinTTL == 0 {
		cfg.Cache.MinTTL = 10
	}
	if cfg.Cache.MaxTTL == 0 {
		cfg.Cache.MaxTTL = 86400
	}
	if cfg.Cache.NegTTL == 0 {
		cfg.Cache.NegTTL = 300
	}
	if cfg.Cache.LazyCacheTTL == 0 {
		cfg.Cache.LazyCacheTTL = 3600
	}
	if cfg.Cache.PrefetchThreshold == 0 {
		cfg.Cache.PrefetchThreshold = 5
	}

	// 日志默认值
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}

	// 协议默认值
	for i := range cfg.Upstreams.Default {
		if cfg.Upstreams.Default[i].Protocol == "" {
			cfg.Upstreams.Default[i].Protocol = "udp"
		}
	}
	for groupName := range cfg.Upstreams.Groups {
		for i := range cfg.Upstreams.Groups[groupName] {
			if cfg.Upstreams.Groups[groupName][i].Protocol == "" {
				cfg.Upstreams.Groups[groupName][i].Protocol = "udp"
			}
		}
	}
}
