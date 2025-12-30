package executor

import (
	"fmt"
	"net"
	"net/http"
	"time"
)

// waitReady 阻塞等待服务就绪
func waitReady(typeStr, target string, timeoutSec int) error {
	if typeStr == "" || typeStr == "none" {
		return nil
	}

	if timeoutSec <= 0 {
		timeoutSec = 30
	} // 默认超时
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)

	for time.Now().Before(deadline) {
		if checkOnce(typeStr, target) {
			return nil // 就绪
		}
		time.Sleep(1 * time.Second) // 1秒轮询一次
	}

	return fmt.Errorf("wait ready timeout (%ds)", timeoutSec)
}

func checkOnce(typeStr, target string) bool {
	switch typeStr {
	case "time":
		// 如果是时间策略，target 是秒数，其实在上层逻辑处理更好，这里为了统一接口
		// 实际 waitReady 循环对于 time 策略不适用，需单独处理
		return false
	case "tcp":
		conn, err := net.DialTimeout("tcp", target, 500*time.Millisecond)
		if err == nil {
			conn.Close()
			return true
		}
	case "http":
		client := http.Client{Timeout: 500 * time.Millisecond}
		resp, err := client.Get(target)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				return true
			}
		}
	}
	return false
}
