package utils

import (
	"net"
	"net/http"
	"strings"
)

// GetClientIP 获取请求的真实 IP
// 1. 去除端口号
// 2. 识别 X-Forwarded-For (防止 Nginx 代理后全是内网 IP)
// 3. 将 IPv6 Loopback 转为 IPv4 习惯
func GetClientIP(r *http.Request) string {
	// 1. 尝试从 X-Forwarded-For 获取 (生产环境 Nginx/LB)
	xForwardedFor := r.Header.Get("X-Forwarded-For")
	if xForwardedFor != "" {
		// X-Forwarded-For 可能包含多个 IP (client, proxy1, proxy2...)，取第一个
		parts := strings.Split(xForwardedFor, ",")
		return strings.TrimSpace(parts[0])
	}

	// 2. 尝试 X-Real-IP
	xRealIP := r.Header.Get("X-Real-IP")
	if xRealIP != "" {
		return xRealIP
	}

	// 3. 回退到 RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// 如果 RemoteAddr 本身没有端口（极少见），直接返回
		ip = r.RemoteAddr
	}

	// 4.不仅去掉了端口，为了美观，将 [::1] 转回 127.0.0.1
	if ip == "::1" {
		return "127.0.0.1"
	}

	return ip
}
