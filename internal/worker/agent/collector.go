package agent

import (
	"fmt"
	"net"
	"path/filepath"
	"runtime"
	"time"

	"ops-system/internal/worker/utils"
	"ops-system/pkg/protocol"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	gNet "github.com/shirou/gopsutil/v3/net"
)

// 全局变量用于计算网速
var (
	lastNetStat gNet.IOCountersStat
	lastNetTime time.Time
)

// GetNodeInfo 采集节点静态信息
func GetNodeInfo() protocol.NodeInfo {
	info := protocol.NodeInfo{
		ID:   utils.GetNodeID(),
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}

	if h, err := host.Info(); err == nil {
		info.Hostname = h.Hostname
		info.OS = fmt.Sprintf("%s %s", h.Platform, h.PlatformVersion)
	}

	if counts, err := cpu.Counts(true); err == nil {
		info.CPUCores = counts
	}

	// 内存保持 MB (符合你的要求：不修改内存逻辑)
	if v, err := mem.VirtualMemory(); err == nil {
		info.MemTotal = v.Total / 1024 / 1024
	}

	// 【修改点 1】: 动态获取磁盘路径
	diskPath := "/"
	if runtime.GOOS == "windows" {
		// 获取当前工作目录的绝对路径
		if absDir, err := filepath.Abs("."); err == nil {
			// 提取卷标 (例如 "D:")
			vol := filepath.VolumeName(absDir)
			if vol != "" {
				diskPath = vol
			} else {
				diskPath = "C:" // 兜底
			}
		} else {
			diskPath = "C:"
		}
	}

	if d, err := disk.Usage(diskPath); err == nil {
		// 【修改点 2】: 返回原始字节 (Bytes)，不再除以 1024
		// 前端逻辑是: row.disk_total / 1024 / 1024 / 1024
		info.DiskTotal = d.Total
	}

	ip, mac := getNetworkInfo()
	info.IP = ip
	info.MacAddr = mac

	return info
}

// GetStatus 采集节点动态监控信息
func GetStatus() protocol.NodeStatus {
	status := protocol.NodeStatus{
		Time: time.Now().Unix(),
	}

	// 1. 内存使用率
	if v, err := mem.VirtualMemory(); err == nil {
		status.MemUsage = v.UsedPercent
	}

	// 2. CPU 使用率
	if c, err := cpu.Percent(0, false); err == nil && len(c) > 0 {
		status.CPUUsage = c[0]
	}

	// 3. 磁盘使用率 (与上面保持一致的逻辑)
	diskPath := "/"
	if runtime.GOOS == "windows" {
		if absDir, err := filepath.Abs("."); err == nil {
			vol := filepath.VolumeName(absDir)
			if vol != "" {
				diskPath = vol
			} else {
				diskPath = "C:"
			}
		} else {
			diskPath = "C:"
		}
	}
	if d, err := disk.Usage(diskPath); err == nil {
		status.DiskUsage = d.UsedPercent
	}

	// 4. 运行时间
	if h, err := host.Info(); err == nil {
		status.Uptime = h.Uptime
	}

	// 5. 网络速率计算
	if ioStats, err := gNet.IOCounters(false); err == nil && len(ioStats) > 0 {
		currentStat := ioStats[0]
		now := time.Now()

		if !lastNetTime.IsZero() {
			duration := now.Sub(lastNetTime).Seconds()
			if duration > 0 {
				bytesRecvDiff := float64(currentStat.BytesRecv - lastNetStat.BytesRecv)
				bytesSentDiff := float64(currentStat.BytesSent - lastNetStat.BytesSent)

				status.NetInSpeed = (bytesRecvDiff / 1024) / duration  // KB/s
				status.NetOutSpeed = (bytesSentDiff / 1024) / duration // KB/s
			}
		}

		lastNetStat = currentStat
		lastNetTime = now
	}

	return status
}

// getNetworkInfo (保持之前的 UDP Dial 优化逻辑不变)
func getNetworkInfo() (string, string) {
	ip := "127.0.0.1"
	mac := ""

	// 尝试 UDP 获取首选出站 IP
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		localAddr := conn.LocalAddr().(*net.UDPAddr)
		ip = localAddr.IP.String()
	} else {
		ip = getFallbackIP()
	}

	// 匹配 MAC
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			var currentIP string
			switch v := addr.(type) {
			case *net.IPNet:
				currentIP = v.IP.String()
			case *net.IPAddr:
				currentIP = v.IP.String()
			}
			if currentIP == ip {
				mac = iface.HardwareAddr.String()
				return ip, mac
			}
		}
	}
	// Fallback MAC
	if mac == "" && len(interfaces) > 0 {
		for _, iface := range interfaces {
			if iface.HardwareAddr.String() != "" {
				mac = iface.HardwareAddr.String()
				break
			}
		}
	}
	return ip, mac
}

func getFallbackIP() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip != nil {
				return ip.String()
			}
		}
	}
	return "127.0.0.1"
}
