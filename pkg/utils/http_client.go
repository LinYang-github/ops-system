package utils

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// 全局 Token 变量
var globalAuthToken string

// InitHTTPClient 初始化 Client 配置
func InitHTTPClient(timeout time.Duration, token string) {
	GlobalClient.Timeout = timeout
	globalAuthToken = token // 保存 Token
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

// PostJSON 发送 JSON 请求 (自动注入 Auth Header)
func PostJSON(url string, data []byte) error {
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	// 【新增】注入 Token
	if globalAuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+globalAuthToken)
	}

	resp, err := GlobalClient.Do(req) // 改用 Do 发送自定义 Request
	if err != nil {
		return err
	}

	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("http status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// 建议增加一个 Get 方法供 Master 查询 Worker 日志列表使用
func Get(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if globalAuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+globalAuthToken)
	}

	resp, err := GlobalClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("http status %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}
