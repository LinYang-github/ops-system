package executor

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"ops-system/pkg/config"
	"ops-system/pkg/protocol"
)

// ReportFunc 定义上报回调函数签名
type ReportFunc func(protocol.InstanceStatusReport)

// Manager 是 Worker 端的执行核心，持有所有状态
type Manager struct {
	// 基础配置
	workDir      string
	pkgCacheDir  string
	logRotateCfg config.LogRotateConfig

	// 状态管理
	downloadLocks sync.Map // key: fileName, value: *sync.Mutex

	// 监控相关状态 (原 ioCache)
	ioCache   map[string]*ioStatCache
	ioCacheMu sync.Mutex

	monitorTicker *time.Ticker // 用于动态调整间隔
	// 外部依赖
	// 使用回调函数解耦 Transport 层
	StatusReporter ReportFunc

	metaCache sync.Map // key: instanceID, value: InstanceMeta
}

// NewManager 构造函数
func NewManager(workDir string, cfg config.LogRotateConfig) *Manager {
	absDir, _ := filepath.Abs(workDir)
	cacheDir := filepath.Join(absDir, "pkg_cache")

	// 确保目录存在
	os.MkdirAll(absDir, 0755)
	os.MkdirAll(cacheDir, 0755)

	return &Manager{
		workDir:      absDir,
		pkgCacheDir:  cacheDir,
		logRotateCfg: cfg,
		ioCache:      make(map[string]*ioStatCache),
	}
}

// SetStatusReporter 设置上报回调 (用于解决循环依赖: Executor -> Transport -> Executor)
func (m *Manager) SetStatusReporter(fn ReportFunc) {
	m.StatusReporter = fn
}

// ReportStatus 主动上报状态的辅助方法
func (m *Manager) ReportStatus(instID, status string, pid int, uptime int64) {
	if m.StatusReporter == nil {
		return
	}
	// 构造基础报告，监控指标填 0
	report := protocol.InstanceStatusReport{
		InstanceID: instID,
		Status:     status,
		PID:        pid,
		Uptime:     uptime,
	}
	m.StatusReporter(report)
}
