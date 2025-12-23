package agent

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"ops-system/pkg/protocol"
	"ops-system/pkg/utils"
)

// StartHeartbeat 启动心跳循环
// masterBaseURL: Master 的地址，例如 "http://192.168.1.100:8080"
// localPort: Worker 自身监听的端口，例如 8081
func StartHeartbeat(masterBaseURL string, localPort int) {
	// 1. 获取静态信息 (启动时只获取一次，如 IP、MAC、操作系统)
	nodeInfo := GetNodeInfo()

	log.Printf("Worker agent started. Target Master: %s, Local Port: %d", masterBaseURL, localPort)

	// 2. 创建定时器，每 5 秒发送一次心跳
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		status := GetStatus()
		reqData := protocol.RegisterRequest{
			Port:   localPort,
			Info:   nodeInfo,
			Status: status,
		}

		jsonData, _ := json.Marshal(reqData)
		url := fmt.Sprintf("%s/api/worker/heartbeat", masterBaseURL)

		// 使用单例方法
		_ = utils.PostJSON(url, jsonData)
	}
}
