package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"ops-system/pkg/protocol"
	// 引入公共工具包
	pkgUtils "ops-system/pkg/utils"

	"github.com/shirou/gopsutil/v3/process"
)

// 用于计算 IO 速率的缓存
type ioStatCache struct {
	BytesRead     uint64
	BytesWrite    uint64
	LastCheckTime time.Time
}

var (
	ioCache         = make(map[string]*ioStatCache)
	cachedMasterURL string

	monitorTicker *time.Ticker
	monitorMu     sync.Mutex
)

// StartMonitor 启动后台监控协程
func StartMonitor(masterURL string, interval time.Duration) {
	cachedMasterURL = masterURL

	if interval <= 0 {
		interval = 3 * time.Second
	}
	monitorTicker = time.NewTicker(interval)

	go func() {
		for range monitorTicker.C {
			checkAndReport(masterURL)
		}
	}()
}

// UpdateMonitorInterval 动态更新监控间隔
func UpdateMonitorInterval(seconds int64) {
	if seconds <= 0 || monitorTicker == nil {
		return
	}
	monitorTicker.Reset(time.Duration(seconds) * time.Second)
}

// ReportStatus 供 Handler 手动调用
func ReportStatus(instID, status string, pid int, uptime int64) {
	if cachedMasterURL == "" {
		return
	}
	reportStatus(cachedMasterURL, instID, status, pid, uptime, 0, 0, 0, 0)
}

func checkAndReport(masterURL string) {
	// 获取所有本地实例目录
	instances := GetAllLocalInstances()

	for _, inst := range instances {
		// 1. 读取 PID 文件
		pidPath := filepath.Join(inst.WorkDir, "pid")
		data, err := os.ReadFile(pidPath)

		// 【核心修复点 1】: 如果 PID 文件不存在，说明是 Stopped 状态
		// 之前是直接 continue，导致 Master 状态无法被纠正
		if err != nil {
			delete(ioCache, inst.InstanceID)
			// 主动上报 stopped，纠正 Master 的状态
			reportStatus(masterURL, inst.InstanceID, "stopped", 0, 0, 0, 0, 0, 0)
			continue
		}

		pidInt, _ := strconv.Atoi(string(data))
		if pidInt <= 0 {
			// PID 内容无效，也报 stopped
			reportStatus(masterURL, inst.InstanceID, "stopped", 0, 0, 0, 0, 0, 0)
			continue
		}

		// 2. 检查进程是否存在
		proc, err := process.NewProcess(int32(pidInt))
		if err != nil {
			// 进程不存在 (僵尸 PID 文件)，上报 stopped
			reportStatus(masterURL, inst.InstanceID, "stopped", 0, 0, 0, 0, 0, 0)
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

		// IO 计算
		ioCounters, _ := proc.IOCounters()
		var ioReadSpeed, ioWriteSpeed uint64

		if ioCounters != nil {
			now := time.Now()
			if last, ok := ioCache[inst.InstanceID]; ok {
				duration := now.Sub(last.LastCheckTime).Seconds()
				if duration > 0 {
					if ioCounters.ReadBytes >= last.BytesRead {
						ioReadSpeed = uint64(float64(ioCounters.ReadBytes-last.BytesRead) / duration / 1024)
					}
					if ioCounters.WriteBytes >= last.BytesWrite {
						ioWriteSpeed = uint64(float64(ioCounters.WriteBytes-last.BytesWrite) / duration / 1024)
					}
				}
				last.BytesRead = ioCounters.ReadBytes
				last.BytesWrite = ioCounters.WriteBytes
				last.LastCheckTime = now
			} else {
				ioCache[inst.InstanceID] = &ioStatCache{
					BytesRead:     ioCounters.ReadBytes,
					BytesWrite:    ioCounters.WriteBytes,
					LastCheckTime: now,
				}
			}
		}

		// 4. 发送 Running 上报
		reportStatus(masterURL, inst.InstanceID, "running", pidInt, startTimeUnix, cpuPercent, memUsageMB, ioReadSpeed, ioWriteSpeed)
	}
}

func reportStatus(masterBaseURL, instID, status string, pid int, uptime int64, cpu float64, mem, ioRead, ioWrite uint64) {
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

	url := fmt.Sprintf("%s/api/instance/status_report", masterBaseURL)
	jsonData, _ := json.Marshal(report)

	// 使用全局 Client 发送
	pkgUtils.PostJSON(url, jsonData)
}
