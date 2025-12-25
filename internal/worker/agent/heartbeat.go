package agent

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"ops-system/internal/worker/executor"
	"ops-system/pkg/protocol"
	pkgUtils "ops-system/pkg/utils" // 引用公共工具
)

// StartHeartbeat 启动心跳
func StartHeartbeat(masterBaseURL string, localPort int, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}

	nodeInfo := GetNodeInfo()
	log.Printf("Heartbeat started. Target: %s, Interval: %v", masterBaseURL, interval)

	ticker := time.NewTicker(interval)
	currentHeartbeatInterval := int64(interval.Seconds())
	currentMonitorInterval := int64(3)

	for {
		select {
		case <-ticker.C:
			status := GetStatus()
			reqData := protocol.RegisterRequest{
				Port:   localPort,
				Info:   nodeInfo,
				Status: status,
			}

			url := fmt.Sprintf("%s/api/worker/heartbeat", masterBaseURL)

			// 1. 发送心跳
			resp, err := sendHeartbeatRequest(url, reqData)
			if err != nil {
				// 打印错误以便调试 (如 401 错误)
				log.Printf("[Heartbeat Error] %v", err)
				continue
			}

			// 2. 动态配置更新
			if resp.HeartbeatInterval > 0 && resp.HeartbeatInterval != currentHeartbeatInterval {
				log.Printf("[Config] Heartbeat interval changed: %ds -> %ds", currentHeartbeatInterval, resp.HeartbeatInterval)
				currentHeartbeatInterval = resp.HeartbeatInterval
				ticker.Reset(time.Duration(currentHeartbeatInterval) * time.Second)
			}

			if resp.MonitorInterval > 0 && resp.MonitorInterval != currentMonitorInterval {
				log.Printf("[Config] Monitor interval changed: %ds -> %ds", currentMonitorInterval, resp.MonitorInterval)
				currentMonitorInterval = resp.MonitorInterval
				executor.UpdateMonitorInterval(currentMonitorInterval)
			}
		}
	}
}

func sendHeartbeatRequest(url string, data protocol.RegisterRequest) (*protocol.HeartbeatResponse, error) {
	jsonData, _ := json.Marshal(data)

	// 【关键修复】使用 pkgUtils.Post，它会自动添加 Authorization Header
	// 之前使用 GlobalClient.Post 是无法带上 Header 的
	body, err := pkgUtils.Post(url, jsonData)

	if err != nil {
		return nil, err
	}

	// 尝试解析标准响应结构 {code: 0, msg: "...", data: {...}}
	var envelope struct {
		Code int                        `json:"code"`
		Msg  string                     `json:"msg"`
		Data protocol.HeartbeatResponse `json:"data"`
	}

	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("parse json failed: %v | Raw: %s", err, string(body))
	}

	// 检查业务错误码
	if envelope.Code != 0 {
		return nil, fmt.Errorf("biz error code %d: %s", envelope.Code, envelope.Msg)
	}

	return &envelope.Data, nil
}
