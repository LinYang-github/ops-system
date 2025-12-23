package agent

import (
	"fmt"
	"net"
	"runtime"
	"time"

	"ops-system/pkg/protocol"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	gNet "github.com/shirou/gopsutil/v3/net" // 给 gopsutil 的 net 包起个别名，避免和标准库 net 冲突
)

// 全局变量用于计算网速
var (
	lastNetStat gNet.IOCountersStat
	lastNetTime time.Time
)

// GetNodeInfo 采集节点静态信息 (仅在启动/注册时调用一次)
func GetNodeInfo() protocol.NodeInfo {
	info := protocol.NodeInfo{
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

	if v, err := mem.VirtualMemory(); err == nil {
		info.MemTotal = v.Total / 1024 / 1024
	}

	// 这里的路径视系统而定，Windows 默认监控 C:，Linux 监控 /
	diskPath := "/"
	if runtime.GOOS == "windows" {
		diskPath = "C:"
	}
	if d, err := disk.Usage(diskPath); err == nil {
		info.DiskTotal = d.Total / 1024 / 1024 / 1024
	}

	// 使用新的 IP 获取逻辑
	ip, mac := getNetworkInfo()
	info.IP = ip
	info.MacAddr = mac

	return info
}

// GetStatus 采集节点动态监控信息 (心跳时循环调用)
func GetStatus() protocol.NodeStatus {
	status := protocol.NodeStatus{
		Time: time.Now().Unix(),
	}

	// 1. 内存使用率
	if v, err := mem.VirtualMemory(); err == nil {
		status.MemUsage = v.UsedPercent
	}

	// 2. CPU 使用率
	// Interval 设为 0 表示计算自上次调用以来的平均负载
	if c, err := cpu.Percent(0, false); err == nil && len(c) > 0 {
		status.CPUUsage = c[0]
	}

	// 3. 磁盘使用率
	if d, err := disk.Usage("/"); err == nil {
		status.DiskUsage = d.UsedPercent
	}

	// 4. 系统运行时间 (Uptime)
	if h, err := host.Info(); err == nil {
		status.Uptime = h.Uptime
	}

	// 5. 网络速率计算 (KB/s)
	// 获取所有网卡的流量总和 (pernic=false)
	if ioStats, err := gNet.IOCounters(false); err == nil && len(ioStats) > 0 {
		currentStat := ioStats[0]
		now := time.Now()

		// 如果不是第一次采集，则计算差值
		if !lastNetTime.IsZero() {
			duration := now.Sub(lastNetTime).Seconds()
			if duration > 0 {
				// 计算公式: (当前字节 - 上次字节) / 时间间隔 / 1024 = KB/s
				bytesRecvDiff := float64(currentStat.BytesRecv - lastNetStat.BytesRecv)
				bytesSentDiff := float64(currentStat.BytesSent - lastNetStat.BytesSent)

				status.NetInSpeed = (bytesRecvDiff / 1024) / duration
				status.NetOutSpeed = (bytesSentDiff / 1024) / duration
			}
		}

		// 更新状态供下一次计算使用
		lastNetStat = currentStat
		lastNetTime = now
	}

	return status
}

// getNetworkInfo 获取本机首个非回环 IPv4 地址和 MAC 地址
func getNetworkInfo() (string, string) {
	ip := "127.0.0.1"
	mac := ""

	// 1. 尝试通过 UDP Dial 获取首选出站 IP
	// 8.8.8.8 是 Google DNS，这里只是为了让操作系统选择路由，不会真的发包
	// 如果是内网环境，可以换成内网网关或 Master 的 IP
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		localAddr := conn.LocalAddr().(*net.UDPAddr)
		ip = localAddr.IP.String()
	} else {
		// 2. 如果没网，回退到遍历网卡
		ip = getFallbackIP()
	}

	// 3. 获取与该 IP 匹配的 MAC 地址
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		// 跳过 loopback 和 down 的
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

	// 如果没找到匹配的 MAC，随便返回一个非空的 MAC
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

// 备用方案：遍历网卡
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
