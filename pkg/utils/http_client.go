package utils

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// 全局变量保存 Token
var globalAuthToken string

// GlobalClient 全局单例 HTTP Client
var GlobalClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	},
}

// InitHTTPClient 初始化 Client 配置和 Token
func InitHTTPClient(timeout time.Duration, token string) {
	if timeout > 0 {
		GlobalClient.Timeout = timeout
	}
	globalAuthToken = token
}

// SetTimeout 动态设置超时时间 (热更新用)
func SetTimeout(d time.Duration) {
	GlobalClient.Timeout = d
}

// PostJSON 发送 JSON 请求并自动处理连接复用 (无返回值版)
func PostJSON(url string, data []byte) error {
	_, err := Post(url, data)
	return err
}

// Post 发送 POST 请求并返回 Body (核心修复方法)
func Post(url string, data []byte) ([]byte, error) {
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	// 【关键修复】在此处注入 Token
	if globalAuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+globalAuthToken)
	}

	resp, err := GlobalClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("http status %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// Get 发送 GET 请求并返回 Body
func Get(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	// 【关键修复】注入 Token
	if globalAuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+globalAuthToken)
	}

	resp, err := GlobalClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("http status %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}
