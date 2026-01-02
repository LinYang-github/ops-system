package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"ops-system/internal/worker/utils"
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

// getInstanceMeta 读取实例元数据（带缓存）
func (m *Manager) getInstanceMeta(workDir, instID string) InstanceMeta {
	// 1. 查缓存
	if val, ok := m.metaCache.Load(instID); ok {
		return val.(InstanceMeta)
	}

	// 2. 读文件 (.meta)
	var meta InstanceMeta
	f, err := os.Open(filepath.Join(workDir, ".meta"))
	if err == nil {
		json.NewDecoder(f).Decode(&meta)
		f.Close()
		m.metaCache.Store(instID, meta) // 写入缓存
	}
	return meta
}

// checkAndReport 核心轮询逻辑 (增强版)
func (m *Manager) checkAndReport() {
	if m.StatusReporter == nil {
		return
	}

	instances := m.GetAllLocalInstances()

	for _, inst := range instances {
		// 1. 读取 Service Manifest (为了获取进程名特征)
		mf, _ := readManifest(inst.WorkDir)
		exeName := ""
		if mf != nil {
			if mf.ProcessName != "" {
				exeName = mf.ProcessName
			} else {
				exeName = filepath.Base(mf.Entrypoint)
			}
		}

		pidPath := filepath.Join(inst.WorkDir, "pid")
		data, err := os.ReadFile(pidPath)

		// ----------------------------------------------------
		// 情况 A: 没有 PID 文件 -> 视为停止
		// ----------------------------------------------------
		if err != nil {
			m.clearIOCache(inst.InstanceID)
			// [修复] 传入 inst.WorkDir
			m.doReport(inst.InstanceID, "stopped", 0, 0, 0, 0, 0, 0, inst.WorkDir)
			continue
		}

		pidInt, _ := strconv.Atoi(string(data))
		if pidInt <= 0 {
			// PID 内容无效 -> 清理文件并上报停止
			os.Remove(pidPath)
			// [修复] 传入 inst.WorkDir
			m.doReport(inst.InstanceID, "stopped", 0, 0, 0, 0, 0, 0, inst.WorkDir)
			continue
		}

		// ----------------------------------------------------
		// 情况 B: 有 PID 文件 -> 检查进程是否存在 (OS Level)
		// ----------------------------------------------------
		proc, err := process.NewProcess(int32(pidInt))

		// 1. 进程根本不存在 (Zombie File)
		if err != nil {
			// log.Printf("[Monitor] %s: PID %d not found. Cleaning.", inst.InstanceID, pidInt)
			os.Remove(pidPath) // [自愈] 删除僵尸 PID 文件
			m.clearIOCache(inst.InstanceID)
			// [修复] 传入 inst.WorkDir
			m.doReport(inst.InstanceID, "stopped", 0, 0, 0, 0, 0, 0, inst.WorkDir)
			continue
		}

		// 2. 进程存在，但特征不匹配 (PID Reuse)
		// [自愈] 解决“状态错乱”：防止接管了不属于自己的进程
		if !m.validateProcessIdentity(proc, inst.WorkDir, exeName) {
			// log.Printf("[Monitor] %s: PID %d mismatch. Cleaning.", inst.InstanceID, pidInt)
			os.Remove(pidPath) // 删除无效文件
			m.clearIOCache(inst.InstanceID)
			// [修复] 传入 inst.WorkDir
			m.doReport(inst.InstanceID, "stopped", 0, 0, 0, 0, 0, 0, inst.WorkDir)
			continue
		}

		// ----------------------------------------------------
		// 情况 C: 校验通过 -> 采集数据
		// ----------------------------------------------------
		cpuPercent, _ := proc.CPUPercent()

		memInfo, _ := proc.MemoryInfo()
		memUsageMB := uint64(0)
		if memInfo != nil {
			memUsageMB = memInfo.RSS / 1024 / 1024
		}

		createTime, _ := proc.CreateTime()
		startTimeUnix := createTime / 1000

		ioReadSpeed, ioWriteSpeed := m.calculateIORate(inst.InstanceID, proc)

		// [修复] 传入 inst.WorkDir
		m.doReport(inst.InstanceID, "running", pidInt, startTimeUnix, cpuPercent, memUsageMB, ioReadSpeed, ioWriteSpeed, inst.WorkDir)
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
// [修改] 增加 workDir 参数，用于读取 .meta
func (m *Manager) doReport(instID, status string, pid int, uptime int64, cpu float64, mem, ioRead, ioWrite uint64, workDir string) {

	// 获取元数据 (SystemID, ServiceName 等)
	meta := m.getInstanceMeta(workDir, instID)

	report := protocol.InstanceStatusReport{
		InstanceID: instID,
		Status:     status,
		PID:        pid,
		Uptime:     uptime,
		CpuUsage:   cpu,
		MemUsage:   mem,
		IoRead:     ioRead,
		IoWrite:    ioWrite,
		// 填充元数据，实现 Master 自愈
		SystemID:       meta.SystemID,
		ServiceName:    meta.ServiceName,
		ServiceVersion: meta.ServiceVersion,
		NodeID:         utils.GetNodeID(),
	}

	if m.StatusReporter != nil {
		m.StatusReporter(report)
	}
}
