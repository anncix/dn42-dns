package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 主配置结构
type Config struct {
	Listen    []ListenConfig   `yaml:"listen"`
	Upstreams UpstreamConfig   `yaml:"upstreams"`
	Routing   RoutingConfig    `yaml:"routing"`
	Cache     CacheConfig      `yaml:"cache"`
	Rdns      RdnsConfig       `yaml:"rdns"`
	Mail      MailConfig       `yaml:"mail"`
	API       APIConfig        `yaml:"api"`
	HA        HAConfig         `yaml:"ha"`
	Log       LogConfig        `yaml:"log"`
}

// APIConfig HTTP管理API配置
type APIConfig struct {
	Enabled bool   `yaml:"enabled"`
	Addr    string `yaml:"addr"` // 监听地址，如 ":8080"
}

// HAConfig 高可用主从配置
type HAConfig struct {
	Mode     string   `yaml:"mode"`      // standalone / master / slave
	Master   string   `yaml:"master"`    // 主节点地址（从节点用），如 "127.0.0.1:8080"
	Slaves   []string `yaml:"slaves"`    // 从节点列表（主节点用）
	SyncInt  int      `yaml:"sync_interval"` // 同步间隔（秒）
}

// ListenConfig 监听配置
type ListenConfig struct {
	Addr     string `yaml:"addr"`
	Protocol string `yaml:"protocol"` // udp / tcp
}

// UpstreamConfig 上游配置
type UpstreamConfig struct {
	Default []UpstreamServer            `yaml:"default"`
	Groups  map[string][]UpstreamServer `yaml:"groups"`
}

// UpstreamServer 上游服务器
type UpstreamServer struct {
	Addr     string `yaml:"addr"`
	Name     string `yaml:"name"`
	Protocol string `yaml:"protocol,omitempty"` // udp / tcp
}

// RoutingConfig 分流配置
type RoutingConfig struct {
	DomainSuffix map[string][]string `yaml:"domain_suffix"`
	DefaultGroup string              `yaml:"default_group"`
}

// CacheConfig 缓存配置
type CacheConfig struct {
	Enabled bool   `yaml:"enabled"`
	MaxSize int    `yaml:"max_size"`
	MinTTL  uint32 `yaml:"min_ttl"`
	MaxTTL  uint32 `yaml:"max_ttl"`
	NegTTL  uint32 `yaml:"neg_ttl"`
}

// RdnsConfig 反向DNS配置
type RdnsConfig struct {
	Enabled         bool              `yaml:"enabled"`
	AllowedNetworks []string          `yaml:"allowed_networks"`
	LocalRecords    map[string]string `yaml:"local_records"`
}

// MailConfig 邮局DNS配置
type MailConfig struct {
	Enabled   bool              `yaml:"enabled"`
	TTL       uint32            `yaml:"ttl"`
	MX        []MXRecord        `yaml:"mx"`      // MX 记录
	A         map[string]string `yaml:"a"`       // 邮件服务器 A 记录
	SPF       map[string]string `yaml:"spf"`     // SPF 记录（TXT）
	DKIM      map[string]string `yaml:"dkim"`    // DKIM 记录（TXT）
	DMARC     map[string]string `yaml:"dmarc"`   // DMARC 记录（TXT）
}

// MXRecord MX 记录
type MXRecord struct {
	Domain   string `yaml:"domain"`   // 域名，如 example.com
	Server   string `yaml:"server"`   // 邮件服务器，如 mail.example.com
	Priority uint16 `yaml:"priority"` // 优先级，数字越小越优先
}

// LogConfig 日志配置
type LogConfig struct {
	Level    string `yaml:"level"`
	QueryLog bool   `yaml:"query_log"`
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
			{Addr: "223.5.5.5:53", Name: "阿里DNS", Protocol: "udp"},
			{Addr: "119.29.29.29:53", Name: "腾讯DNS", Protocol: "udp"},
		}
	}

	// 默认分流组
	if cfg.Routing.DefaultGroup == "" {
		cfg.Routing.DefaultGroup = "default"
	}

	// 缓存默认值
	if cfg.Cache.MaxSize == 0 {
		cfg.Cache.MaxSize = 20000
	}
	if cfg.Cache.MinTTL == 0 {
		cfg.Cache.MinTTL = 60
	}
	if cfg.Cache.MaxTTL == 0 {
		cfg.Cache.MaxTTL = 86400
	}
	if cfg.Cache.NegTTL == 0 {
		cfg.Cache.NegTTL = 600
	}

	// 日志默认值
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}

	// API 默认值（默认只监听本地，更安全）
	if cfg.API.Addr == "" {
		cfg.API.Addr = "127.0.0.1:8080"
	}

	// HA 默认值
	if cfg.HA.Mode == "" {
		cfg.HA.Mode = "standalone"
	}
	if cfg.HA.SyncInt == 0 {
		cfg.HA.SyncInt = 30
	}

	// 邮局DNS 默认值
	if cfg.Mail.TTL == 0 {
		cfg.Mail.TTL = 3600
	}

	// 上游协议默认值
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
