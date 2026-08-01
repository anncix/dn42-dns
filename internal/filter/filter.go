package filter

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"smartdns/internal/config"
)

// AdFilter 广告过滤器
type AdFilter struct {
	cfg      *config.AdBlockConfig
	radix    *RadixTree  // 黑名单基数树
	whitelist *RadixTree // 白名单基数树
	mu       sync.RWMutex
	stats    FilterStats
}

// FilterStats 过滤器统计
type FilterStats struct {
	TotalQueries   uint64
	BlockedQueries uint64
	Whitelisted    uint64
}

// RadixTree 基数树（域名反转存储，实现后缀匹配）
type RadixTree struct {
	root *radixNode
	size int
}

type radixNode struct {
	children map[byte]*radixNode
	isEnd    bool // 是否是一个完整规则的结尾
	rule     string
}

// NewRadixTree 创建基数树
func NewRadixTree() *RadixTree {
	return &RadixTree{
		root: &radixNode{
			children: make(map[byte]*radixNode),
		},
	}
}

// Insert 插入域名规则
func (t *RadixTree) Insert(domain string) {
	// 域名反转存储，将后缀匹配转化为前缀匹配
	reversed := reverseDomain(domain)

	node := t.root
	for i := 0; i < len(reversed); i++ {
		c := reversed[i]
		if node.children[c] == nil {
			node.children[c] = &radixNode{
				children: make(map[byte]*radixNode),
			}
		}
		node = node.children[c]
	}

	if !node.isEnd {
		node.isEnd = true
		node.rule = domain
		t.size++
	}
}

// Search 搜索域名是否匹配（最长后缀匹配）
func (t *RadixTree) Search(domain string) (bool, string) {
	reversed := reverseDomain(domain)

	node := t.root
	var lastMatch string
	var found bool

	for i := 0; i < len(reversed); i++ {
		c := reversed[i]
		if node.children[c] == nil {
			break
		}
		node = node.children[c]
		if node.isEnd {
			lastMatch = node.rule
			found = true
		}
	}

	return found, lastMatch
}

// Size 返回规则数量
func (t *RadixTree) Size() int {
	return t.size
}

// reverseDomain 反转域名，用于将后缀匹配转为前缀匹配
// 例如: "ads.example.com" -> "moc.elpmaxe.sda."
func reverseDomain(domain string) string {
	// 标准化：去除末尾的点
	domain = strings.TrimSuffix(domain, ".")

	// 反转字符串
	runes := []rune(domain)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}

	// 添加末尾点作为分隔符（确保完整匹配）
	return string(runes) + "."
}

// NewAdFilter 创建广告过滤器
func NewAdFilter(cfg *config.AdBlockConfig) (*AdFilter, error) {
	f := &AdFilter{
		cfg:       cfg,
		radix:     NewRadixTree(),
		whitelist: NewRadixTree(),
	}

	if !cfg.Enabled {
		return f, nil
	}

	// 加载黑名单
	if err := f.loadBlacklists(); err != nil {
		return nil, fmt.Errorf("加载黑名单失败: %w", err)
	}

	// 加载白名单
	if err := f.loadWhitelists(); err != nil {
		return nil, fmt.Errorf("加载白名单失败: %w", err)
	}

	// 加载自定义规则
	for _, domain := range cfg.CustomBlacklist {
		f.radix.Insert(strings.TrimSpace(domain))
	}
	for _, domain := range cfg.CustomWhitelist {
		f.whitelist.Insert(strings.TrimSpace(domain))
	}

	return f, nil
}

// loadBlacklists 加载黑名单文件
func (f *AdFilter) loadBlacklists() error {
	for _, file := range f.cfg.BlacklistFiles {
		domains, err := f.loadRuleFile(file)
		if err != nil {
			fmt.Printf("警告: 加载黑名单文件 %s 失败: %v\n", file, err)
			continue
		}
		for _, domain := range domains {
			f.radix.Insert(domain)
		}
		fmt.Printf("加载黑名单 %s: %d 条规则\n", file, len(domains))
	}
	return nil
}

// loadWhitelists 加载白名单文件
func (f *AdFilter) loadWhitelists() error {
	for _, file := range f.cfg.WhitelistFiles {
		domains, err := f.loadRuleFile(file)
		if err != nil {
			fmt.Printf("警告: 加载白名单文件 %s 失败: %v\n", file, err)
			continue
		}
		for _, domain := range domains {
			f.whitelist.Insert(domain)
		}
		fmt.Printf("加载白名单 %s: %d 条规则\n", file, len(domains))
	}
	return nil
}

// loadRuleFile 加载规则文件（支持本地文件和HTTP URL）
func (f *AdFilter) loadRuleFile(path string) ([]string, error) {
	var scanner *bufio.Scanner

	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		// 远程文件
		resp, err := http.Get(path)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		scanner = bufio.NewScanner(resp.Body)
	} else {
		// 本地文件
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		scanner = bufio.NewScanner(file)
	}

	var domains []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		domain := parseRuleLine(line)
		if domain != "" {
			domains = append(domains, domain)
		}
	}

	return domains, scanner.Err()
}

// parseRuleLine 解析规则行，支持多种格式
func parseRuleLine(line string) string {
	// 空行或注释行
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
		return ""
	}

	// AdGuard格式: ||example.com^
	if strings.HasPrefix(line, "||") {
		line = strings.TrimPrefix(line, "||")
		line = strings.TrimRight(line, "^$")
		// 去除修饰符
		if idx := strings.Index(line, "$"); idx != -1 {
			line = line[:idx]
		}
		return strings.TrimSpace(line)
	}

	// hosts格式: 0.0.0.0 example.com 或 127.0.0.1 example.com
	fields := strings.Fields(line)
	if len(fields) >= 2 {
		// 检查第一个字段是否是IP
		if isIPLike(fields[0]) {
			// 第二个字段是域名
			domain := fields[1]
			// 去除注释
			if idx := strings.Index(domain, "#"); idx != -1 {
				domain = domain[:idx]
			}
			return strings.TrimSpace(domain)
		}
	}

	// 纯域名格式
	if strings.Contains(line, ".") {
		// 去除可能的注释
		if idx := strings.Index(line, "#"); idx != -1 {
			line = line[:idx]
		}
		return strings.TrimSpace(line)
	}

	return ""
}

// isIPLike 检查字符串是否像IP地址
func isIPLike(s string) bool {
	// 简单检查：包含点且不全是字母
	if strings.Count(s, ".") == 3 {
		for _, c := range s {
			if c != '.' && (c < '0' || c > '9') {
				return false
			}
		}
		return true
	}
	return false
}

// IsBlocked 检查域名是否被拦截
func (f *AdFilter) IsBlocked(domain string) (bool, string) {
	if !f.cfg.Enabled {
		return false, ""
	}

	f.mu.RLock()
	defer f.mu.RUnlock()

	f.stats.TotalQueries++

	// 先检查白名单
	if whitelisted, rule := f.whitelist.Search(domain); whitelisted {
		f.stats.Whitelisted++
		return false, "whitelist: " + rule
	}

	// 再检查黑名单
	if blocked, rule := f.radix.Search(domain); blocked {
		f.stats.BlockedQueries++
		return true, rule
	}

	return false, ""
}

// GetStats 获取统计信息
func (f *AdFilter) GetStats() FilterStats {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.stats
}

// GetBlacklistSize 获取黑名单规则数
func (f *AdFilter) GetBlacklistSize() int {
	return f.radix.Size()
}

// GetWhitelistSize 获取白名单规则数
func (f *AdFilter) GetWhitelistSize() int {
	return f.whitelist.Size()
}

// Reload 重新加载规则
func (f *AdFilter) Reload() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.radix = NewRadixTree()
	f.whitelist = NewRadixTree()

	if err := f.loadBlacklists(); err != nil {
		return err
	}
	if err := f.loadWhitelists(); err != nil {
		return err
	}

	return nil
}
