package executor

import (
	"os"
	"path/filepath"
	"strconv"
	"time"

	"ops-system/pkg/protocol"

	"github.com/shirou/gopsutil/v3/process"
)

// ioStatCache 用于计算 IO 速率的缓存结构
type ioStatCache struct {
	BytesRead     uint64
	BytesWrite    uint64
	LastCheckTime time.Time
}

// StartMonitor 启动后台监控协程
func (m *Manager) StartMonitor(interval time.Duration) {
	if interval <= 0 {
		interval = 3 * time.Second
	}

	// 保存 ticker 到实例中，以便 UpdateMonitorInterval 可以访问
	m.monitorTicker = time.NewTicker(interval)

	go func() {
		for range m.monitorTicker.C {
			m.checkAndReport()
		}
	}()
}

// UpdateMonitorInterval 动态更新监控间隔 (热更新)
func (m *Manager) UpdateMonitorInterval(seconds int64) {
	if seconds <= 0 || m.monitorTicker == nil {
		return
	}
	m.monitorTicker.Reset(time.Duration(seconds) * time.Second)
}

// checkAndReport 核心轮询逻辑
func (m *Manager) checkAndReport() {
	// 如果没有设置上报回调，采集也没有意义
	if m.StatusReporter == nil {
		return
	}

	// 获取所有本地实例目录 (包括标准和纳管)
	instances := m.GetAllLocalInstances()

	for _, inst := range instances {
		// 1. 读取 PID 文件
		pidPath := filepath.Join(inst.WorkDir, "pid")
		data, err := os.ReadFile(pidPath)

		// 如果 PID 文件不存在，说明是 Stopped 状态
		if err != nil {
			m.clearIOCache(inst.InstanceID)
			// 主动上报 stopped，纠正 Master 的状态
			m.doReport(inst.InstanceID, "stopped", 0, 0, 0, 0, 0, 0)
			continue
		}

		pidInt, _ := strconv.Atoi(string(data))
		if pidInt <= 0 {
			// PID 内容无效
			m.doReport(inst.InstanceID, "stopped", 0, 0, 0, 0, 0, 0)
			continue
		}

		// 2. 检查进程是否存在
		proc, err := process.NewProcess(int32(pidInt))
		if err != nil {
			// 进程不存在 (僵尸 PID 文件)
			m.doReport(inst.InstanceID, "stopped", 0, 0, 0, 0, 0, 0)
			// 可选：清理无效的 PID 文件
			// os.Remove(pidPath)
			continue
		}

		// 3. 采集指标
		cpuPercent, _ := proc.CPUPercent()

		memInfo, _ := proc.MemoryInfo()
		memUsageMB := uint64(0)
		if memInfo != nil {
			memUsageMB = memInfo.RSS / 1024 / 1024
		}

		createTime, _ := proc.CreateTime()
		startTimeUnix := createTime / 1000

		// 4. IO 速率计算
		ioReadSpeed, ioWriteSpeed := m.calculateIORate(inst.InstanceID, proc)

		// 5. 发送 Running 上报
		m.doReport(inst.InstanceID, "running", pidInt, startTimeUnix, cpuPercent, memUsageMB, ioReadSpeed, ioWriteSpeed)
	}
}

// calculateIORate 计算 IO 速率 (带锁)
func (m *Manager) calculateIORate(instID string, proc *process.Process) (uint64, uint64) {
	ioCounters, _ := proc.IOCounters()
	if ioCounters == nil {
		return 0, 0
	}

	m.ioCacheMu.Lock()
	defer m.ioCacheMu.Unlock()

	var readSpeed, writeSpeed uint64
	now := time.Now()

	if last, ok := m.ioCache[instID]; ok {
		duration := now.Sub(last.LastCheckTime).Seconds()
		if duration > 0 {
			if ioCounters.ReadBytes >= last.BytesRead {
				readSpeed = uint64(float64(ioCounters.ReadBytes-last.BytesRead) / duration / 1024) // KB/s
			}
			if ioCounters.WriteBytes >= last.BytesWrite {
				writeSpeed = uint64(float64(ioCounters.WriteBytes-last.BytesWrite) / duration / 1024) // KB/s
			}
		}
		// 更新缓存
		last.BytesRead = ioCounters.ReadBytes
		last.BytesWrite = ioCounters.WriteBytes
		last.LastCheckTime = now
	} else {
		// 首次初始化
		m.ioCache[instID] = &ioStatCache{
			BytesRead:     ioCounters.ReadBytes,
			BytesWrite:    ioCounters.WriteBytes,
			LastCheckTime: now,
		}
	}

	return readSpeed, writeSpeed
}

// clearIOCache 清理已停止实例的 IO 缓存
func (m *Manager) clearIOCache(instID string) {
	m.ioCacheMu.Lock()
	defer m.ioCacheMu.Unlock()
	delete(m.ioCache, instID)
}

// doReport 组装并发送报告
func (m *Manager) doReport(instID, status string, pid int, uptime int64, cpu float64, mem, ioRead, ioWrite uint64) {
	report := protocol.InstanceStatusReport{
		InstanceID: instID,
		Status:     status,
		PID:        pid,
		Uptime:     uptime,
		CpuUsage:   cpu,
		MemUsage:   mem,
		IoRead:     ioRead,
		IoWrite:    ioWrite,
	}

	if m.StatusReporter != nil {
		m.StatusReporter(report)
	}
}
