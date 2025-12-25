package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	// 这里引用的是 worker 本地的 utils (如果用到) 或 pkg/utils
	// 注意：这里 reportStatus 用到的 PostJSON 其实应该来自 pkg/utils
	// 为了避免混乱，我们在这里显式引入 pkg/utils
	pkgUtils "ops-system/pkg/utils"

	"ops-system/pkg/protocol"

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

	// 【新增】用于动态更新监控频率
	monitorTicker *time.Ticker
	monitorMu     sync.Mutex
)

// StartMonitor 启动后台监控协程
func StartMonitor(masterURL string, interval time.Duration) {
	cachedMasterURL = masterURL

	// 初始化 Ticker
	if interval <= 0 {
		interval = 3 * time.Second
	}
	monitorTicker = time.NewTicker(interval)

	go func() {
		// 监听 Ticker
		for range monitorTicker.C {
			checkAndReport(masterURL)
		}
	}()
}

// 【新增】UpdateMonitorInterval 动态更新监控间隔 (修复报错的核心)
func UpdateMonitorInterval(seconds int64) {
	if seconds <= 0 || monitorTicker == nil {
		return
	}
	// Reset 是线程安全的，直接重置下一次触发时间
	monitorTicker.Reset(time.Duration(seconds) * time.Second)
}

// ReportStatus 供 Handler 在异步任务完成时调用
func ReportStatus(instID, status string, pid int, uptime int64) {
	if cachedMasterURL == "" {
		return
	}
	reportStatus(cachedMasterURL, instID, status, pid, uptime, 0, 0, 0, 0)
}

func checkAndReport(masterURL string) {
	instances := GetAllLocalInstances()

	for _, inst := range instances {
		pidPath := filepath.Join(inst.WorkDir, "pid")
		data, err := os.ReadFile(pidPath)
		if err != nil {
			delete(ioCache, inst.InstanceID)
			continue
		}

		pidInt, _ := strconv.Atoi(string(data))
		if pidInt <= 0 {
			continue
		}

		proc, err := process.NewProcess(int32(pidInt))
		if err != nil {
			reportStatus(masterURL, inst.InstanceID, "stopped", 0, 0, 0, 0, 0, 0)
			continue
		}

		cpuPercent, _ := proc.CPUPercent()

		memInfo, _ := proc.MemoryInfo()
		memUsageMB := uint64(0)
		if memInfo != nil {
			memUsageMB = memInfo.RSS / 1024 / 1024
		}

		createTime, _ := proc.CreateTime()
		startTimeUnix := createTime / 1000

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

	// 【修改】使用 pkgUtils (即 ops-system/pkg/utils)
	pkgUtils.PostJSON(url, jsonData)
}
