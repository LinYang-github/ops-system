package utils

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

func InitHTTPClient(timeout time.Duration) {
	GlobalClient.Timeout = timeout
}

// GlobalClient 全局单例 HTTP Client，配置长连接池
var GlobalClient = &http.Client{
	Timeout: 10 * time.Second, // 设置一个合理的超时
	Transport: &http.Transport{
		MaxIdleConns:        100,              // 总空闲连接数
		MaxIdleConnsPerHost: 20,               // 对每个 Host (Worker IP) 保持的空闲连接数
		IdleConnTimeout:     90 * time.Second, // 空闲连接保持时间
		DisableKeepAlives:   false,            // 开启 Keep-Alive

		// 拨号优化
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	},
}

// PostJSON 发送 JSON 请求并自动处理连接复用
// 如果状态码不是 200，会返回错误
func PostJSON(url string, data []byte) error {
	resp, err := GlobalClient.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return err
	}

	// 【关键】必须读取并关闭 Body，连接才能归还到池中
	// 如果需要读取 Worker 返回的具体错误信息，可以在这里读取
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("http status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
