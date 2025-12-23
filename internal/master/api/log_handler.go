package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"

	// "ops-system/internal/master/store" // ❌ 删除旧引用
	"ops-system/pkg/protocol"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// handleGetOpLogs 分页查询操作日志
// POST /api/logs
func handleGetOpLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}

	var req protocol.LogQueryReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", 400)
		return
	}

	// 使用全局变量 logManager (在 server.go 中定义)
	logs, err := logManager.GetLogs(req.Page, req.PageSize, req.Keyword)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}

// handleGetInstanceLogFiles 获取实例日志文件列表
// GET /api/instance/logs/files?instance_id=...
func handleGetInstanceLogFiles(w http.ResponseWriter, r *http.Request) {
	instID := r.URL.Query().Get("instance_id")
	if instID == "" {
		http.Error(w, "missing instance_id", 400)
		return
	}

	// 1. 获取实例
	inst, ok := instManager.GetInstance(instID)
	if !ok {
		http.Error(w, "instance not found", 404)
		return
	}

	// 2. 获取节点 (用于获取 WorkerURL)
	// 【关键修改】使用 nodeManager 替代 store.GetNode
	node, exists := nodeManager.GetNode(inst.NodeIP)
	if !exists {
		http.Error(w, "node offline", 404)
		return
	}

	// 3. 转发请求给 Worker
	// 拼接 URL: http://IP:Port/api/log/files?instance_id=...
	targetURL := fmt.Sprintf("http://%s:%d/api/log/files?instance_id=%s", node.IP, node.Port, instID)

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(targetURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("worker connect failed: %v", err), 502)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")

	var result protocol.LogFilesResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		http.Error(w, "worker response error", 502)
		return
	}
	json.NewEncoder(w).Encode(result)
}

// handleInstanceLogStream 代理日志 WebSocket
// GET /api/instance/logs/stream?instance_id=...&log_key=...
func handleInstanceLogStream(w http.ResponseWriter, r *http.Request) {
	instID := r.URL.Query().Get("instance_id")
	logKey := r.URL.Query().Get("log_key")

	inst, ok := instManager.GetInstance(instID)
	if !ok {
		http.Error(w, "Instance not found", 404)
		return
	}

	// 【关键修改】使用 nodeManager
	node, exists := nodeManager.GetNode(inst.NodeIP)
	if !exists {
		http.Error(w, "Node offline", 404)
		return
	}

	// 构造 Worker WS URL: ws://IP:Port/api/log/ws...
	workerWsURL := fmt.Sprintf("ws://%s:%d/api/log/ws?instance_id=%s&log_key=%s",
		node.IP, node.Port, instID, url.QueryEscape(logKey))

	// Dial Worker
	workerConn, _, err := websocket.DefaultDialer.Dial(workerWsURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Connect worker failed: %v", err), 502)
		return
	}
	defer workerConn.Close()

	// 升级前端连接
	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer clientConn.Close()

	// 双向管道 (Pipe)
	errChan := make(chan error, 2)

	// Worker -> Frontend
	go func() {
		for {
			mt, message, err := workerConn.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}
			if err := clientConn.WriteMessage(mt, message); err != nil {
				errChan <- err
				return
			}
		}
	}()

	// Frontend -> Worker
	go func() {
		for {
			mt, message, err := clientConn.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}
			if err := workerConn.WriteMessage(mt, message); err != nil {
				errChan <- err
				return
			}
		}
	}()

	<-errChan
}
