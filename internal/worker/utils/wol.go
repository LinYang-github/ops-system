package utils

import (
	"fmt"
	"net"
	"strings"
)

// SendMagicPacket 发送 WoL 魔术包
func SendMagicPacket(macAddr string) error {
	// 1. 解析 MAC 地址
	hwAddr, err := net.ParseMAC(macAddr)
	if err != nil {
		return fmt.Errorf("invalid mac address: %v", err)
	}

	// 2. 构建 Magic Packet
	// 格式: 6个 0xFF, 紧接着 16 次 MAC 地址
	var packet []byte
	for i := 0; i < 6; i++ {
		packet = append(packet, 0xFF)
	}
	for i := 0; i < 16; i++ {
		packet = append(packet, hwAddr...)
	}

	// 3. 获取广播地址 (简化版：直接使用全局广播 255.255.255.255)
	// 端口通常使用 9 (Discard) 或 7 (Echo)
	broadcastAddr := "255.255.255.255:9"

	// 4. 发送 UDP 包
	conn, err := net.Dial("udp", broadcastAddr)
	if err != nil {
		return fmt.Errorf("dial udp failed: %v", err)
	}
	defer conn.Close()

	n, err := conn.Write(packet)
	if err != nil {
		return fmt.Errorf("write packet failed: %v", err)
	}
	if n != 102 {
		return fmt.Errorf("magic packet length mismatch: wrote %d bytes", n)
	}

	return nil
}

// IsSameSubnet 简单的网段匹配辅助函数 (IPv4)
// 用于 Master 判断两个节点是否在同一局域网
// 这里简单判断前三段是否一致 (例如 192.168.1.x)
func IsSameSubnet(ip1, ip2 string) bool {
	parts1 := strings.Split(ip1, ".")
	parts2 := strings.Split(ip2, ".")
	if len(parts1) != 4 || len(parts2) != 4 {
		return false
	}
	return parts1[0] == parts2[0] && parts1[1] == parts2[1] && parts1[2] == parts2[2]
}
