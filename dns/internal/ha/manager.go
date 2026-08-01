package ha

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"smartdns/internal/config"
	"smartdns/internal/rdns"
)

// Manager 高可用管理器
type Manager struct {
	cfg      *config.HAConfig
	rdns     *rdns.Handler
	mode     string // standalone / master / slave
	master   string // 主节点地址
	slaves   []string
	running  bool
	stopCh   chan struct{}
	mu       sync.RWMutex

	// 从节点状态（主节点用）
	slaveStatus map[string]SlaveStatus

	// 主节点状态（从节点用）
	masterStatus MasterStatus
}

// SlaveStatus 从节点状态
type SlaveStatus struct {
	Addr      string    `json:"addr"`
	Healthy   bool      `json:"healthy"`
	LastCheck time.Time `json:"last_check"`
	LatencyMs int64     `json:"latency_ms"`
}

// MasterStatus 主节点状态
type MasterStatus struct {
	Addr      string    `json:"addr"`
	Healthy   bool      `json:"healthy"`
	LastCheck time.Time `json:"last_check"`
	LatencyMs int64     `json:"latency_ms"`
}

// NewManager 创建高可用管理器
func NewManager(cfg *config.HAConfig) *Manager {
	m := &Manager{
		cfg:         cfg,
		mode:        cfg.Mode,
		master:      cfg.Master,
		slaves:      cfg.Slaves,
		stopCh:      make(chan struct{}),
		slaveStatus: make(map[string]SlaveStatus),
	}

	// 初始化从节点状态
	for _, addr := range cfg.Slaves {
		m.slaveStatus[addr] = SlaveStatus{Addr: addr}
	}

	return m
}

// SetRdns 设置 rDNS 处理器
func (m *Manager) SetRdns(r *rdns.Handler) {
	m.rdns = r
}

// Mode 获取当前模式
func (m *Manager) Mode() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mode
}

// Start 启动高可用管理
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("HA 管理器已在运行")
	}

	switch m.mode {
	case "master":
		fmt.Printf("[HA] 主节点模式，从节点: %v\n", m.slaves)
		go m.masterLoop()
	case "slave":
		fmt.Printf("[HA] 从节点模式，主节点: %s\n", m.master)
		go m.slaveLoop()
	default:
		fmt.Println("[HA] 单机模式")
	}

	m.running = true
	return nil
}

// Stop 停止高可用管理
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return
	}

	close(m.stopCh)
	m.running = false
}

// masterLoop 主节点循环
func (m *Manager) masterLoop() {
	interval := time.Duration(m.cfg.SyncInt) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 立即执行一次
	m.checkSlaves()

	for {
		select {
		case <-ticker.C:
			m.checkSlaves()
		case <-m.stopCh:
			return
		}
	}
}

// slaveLoop 从节点循环
func (m *Manager) slaveLoop() {
	interval := time.Duration(m.cfg.SyncInt) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 立即执行一次
	m.syncFromMaster()

	for {
		select {
		case <-ticker.C:
			m.syncFromMaster()
		case <-m.stopCh:
			return
		}
	}
}

// checkSlaves 检查从节点健康状态（主节点用）
func (m *Manager) checkSlaves() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, addr := range m.slaves {
		start := time.Now()
		healthy := m.checkNodeHealth(addr)
		latency := time.Since(start).Milliseconds()

		status := SlaveStatus{
			Addr:      addr,
			Healthy:   healthy,
			LastCheck: time.Now(),
			LatencyMs: latency,
		}
		m.slaveStatus[addr] = status

		if healthy {
			fmt.Printf("[HA] 从节点 %s 健康 (延迟 %dms)\n", addr, latency)
		} else {
			fmt.Printf("[HA] 从节点 %s 不健康\n", addr)
		}
	}
}

// syncFromMaster 从主节点同步数据（从节点用）
func (m *Manager) syncFromMaster() {
	m.mu.Lock()
	defer m.mu.Unlock()

	start := time.Now()
	healthy := m.checkNodeHealth(m.master)
	latency := time.Since(start).Milliseconds()

	m.masterStatus = MasterStatus{
		Addr:      m.master,
		Healthy:   healthy,
		LastCheck: time.Now(),
		LatencyMs: latency,
	}

	if !healthy {
		fmt.Printf("[HA] 主节点 %s 不健康\n", m.master)
		return
	}

	fmt.Printf("[HA] 主节点健康 (延迟 %dms)，同步数据...\n", latency)

	// 同步 rDNS 记录
	if m.rdns != nil {
		records, err := m.fetchRdnsRecords(m.master)
		if err != nil {
			fmt.Printf("[HA] 同步 rDNS 记录失败: %v\n", err)
		} else {
			m.rdns.SetLocalRecords(records)
			fmt.Printf("[HA] 同步 rDNS 记录: %d 条\n", len(records))
		}
	}
}

// checkNodeHealth 检查节点健康
func (m *Manager) checkNodeHealth(addr string) bool {
	url := fmt.Sprintf("http://%s/api/health", addr)
	client := &http.Client{
		Timeout: 3 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// fetchRdnsRecords 从节点获取 rDNS 记录
func (m *Manager) fetchRdnsRecords(addr string) (map[string]string, error) {
	url := fmt.Sprintf("http://%s/api/rdns/records", addr)
	client := &http.Client{
		Timeout: 3 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var result struct {
		Records map[string]string `json:"records"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Records, nil
}

// GetSlaveStatus 获取从节点状态（主节点用）
func (m *Manager) GetSlaveStatus() map[string]SlaveStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]SlaveStatus, len(m.slaveStatus))
	for k, v := range m.slaveStatus {
		result[k] = v
	}
	return result
}

// GetMasterStatus 获取主节点状态（从节点用）
func (m *Manager) GetMasterStatus() MasterStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.masterStatus
}
