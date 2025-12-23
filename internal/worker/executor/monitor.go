package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"ops-system/pkg/protocol"
	"ops-system/pkg/utils" // 引入全局 HTTP Client

	"github.com/shirou/gopsutil/v3/process"
)

// 用于计算 IO 速率的缓存
type ioStatCache struct {
	BytesRead     uint64
	BytesWrite    uint64
	LastCheckTime time.Time
}

var (
	ioCache         = make(map[string]*ioStatCache) // key: InstanceID
	cachedMasterURL string                          // 缓存 Master 地址，供 ReportStatus 使用
)

// StartMonitor 启动后台监控协程
func StartMonitor(masterURL string) {
	cachedMasterURL = masterURL
	go func() {
		// 3秒采集一次
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			checkAndReport(masterURL)
		}
	}()
}

// ReportStatus 【导出方法】供 Handler 在异步任务（如部署）完成时手动调用
// 此时不需要监控数据，只上报状态变更
func ReportStatus(instID, status string, pid int, uptime int64) {
	if cachedMasterURL == "" {
		return
	}
	// 填入 0 的监控数据
	reportStatus(cachedMasterURL, instID, status, pid, uptime, 0, 0, 0, 0)
}

// checkAndReport 内部轮询逻辑
func checkAndReport(masterURL string) {
	// 1. 获取所有本地实例 (该函数在 instance_manager.go 中定义)
	instances := GetAllLocalInstances()

	for _, inst := range instances {
		// 2. 读取 PID
		pidPath := filepath.Join(inst.WorkDir, "pid")
		data, err := os.ReadFile(pidPath)
		if err != nil {
			// 没有 PID 文件，说明是停止状态，清理缓存并跳过
			delete(ioCache, inst.InstanceID)
			continue
		}

		pidInt, _ := strconv.Atoi(string(data))
		if pidInt <= 0 {
			continue
		}

		// 3. 获取进程对象
		proc, err := process.NewProcess(int32(pidInt))
		if err != nil {
			// 进程不存在 (僵尸 PID 文件)，视为停止
			// 9个参数补全
			reportStatus(masterURL, inst.InstanceID, "stopped", 0, 0, 0, 0, 0, 0)
			continue
		}

		// 4. 采集指标

		// 4.1 CPU (非阻塞)
		cpuPercent, _ := proc.CPUPercent()

		// 4.2 内存
		memInfo, _ := proc.MemoryInfo()
		memUsageMB := uint64(0)
		if memInfo != nil {
			memUsageMB = memInfo.RSS / 1024 / 1024 // RSS 转 MB
		}

		// 4.3 启动时间
		createTime, _ := proc.CreateTime() // 毫秒
		// 修复之前的未使用变量错误，直接使用
		startTimeUnix := createTime / 1000

		// 4.4 IO 速率计算
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

		// 5. 发送上报 (Running 状态)
		reportStatus(masterURL, inst.InstanceID, "running", pidInt, startTimeUnix, cpuPercent, memUsageMB, ioReadSpeed, ioWriteSpeed)
	}
}

// 内部底层上报逻辑
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

	// 使用全局连接池 Client 发送 (需引入 internal/worker/utils)
	// 忽略错误，因为这是高频上报
	_ = utils.PostJSON(url, jsonData)
}
