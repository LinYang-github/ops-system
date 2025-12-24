package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"ops-system/pkg/code"
	"ops-system/pkg/e"
	"ops-system/pkg/protocol"
	"ops-system/pkg/response"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// GetOpLogs 分页查询操作日志
// POST /api/logs
func (h *ServerHandler) GetOpLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, e.New(code.MethodNotAllowed, "Method not allowed", nil))
		return
	}

	var req protocol.LogQueryReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	// 使用注入的 logMgr
	logs, err := h.logMgr.GetLogs(req.Page, req.PageSize, req.Keyword)
	if err != nil {
		response.Error(w, e.New(code.DatabaseError, "查询日志失败", err))
		return
	}

	response.Success(w, logs)
}

// GetInstanceLogFiles 获取实例日志文件列表
// GET /api/instance/logs/files?instance_id=...
func (h *ServerHandler) GetInstanceLogFiles(w http.ResponseWriter, r *http.Request) {
	instID := r.URL.Query().Get("instance_id")
	if instID == "" {
		response.Error(w, e.New(code.ParamError, "缺少 instance_id", nil))
		return
	}

	// 1. 获取实例 (使用 instMgr)
	inst, ok := h.instMgr.GetInstance(instID)
	if !ok {
		response.Error(w, e.New(code.InstanceNotFound, "实例不存在", nil))
		return
	}

	// 2. 获取节点 (使用 nodeMgr 获取 IP 和 Port)
	node, exists := h.nodeMgr.GetNode(inst.NodeIP)
	if !exists {
		response.Error(w, e.New(code.NodeOffline, "节点离线或不存在", nil))
		return
	}

	// 3. 转发请求给 Worker
	// 拼接 URL: http://IP:Port/api/log/files?instance_id=...
	targetURL := fmt.Sprintf("http://%s:%d/api/log/files?instance_id=%s", node.IP, node.Port, instID)

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(targetURL)
	if err != nil {
		response.Error(w, e.New(code.NetworkError, fmt.Sprintf("连接 Worker 失败: %v", err), err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		response.Error(w, e.New(code.ServerError, fmt.Sprintf("Worker 返回错误码: %d", resp.StatusCode), nil))
		return
	}

	var result protocol.LogFilesResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		response.Error(w, e.New(code.ServerError, "解析 Worker 响应失败", err))
		return
	}

	response.Success(w, result)
}

// InstanceLogStream 代理日志 WebSocket
// GET /api/instance/logs/stream?instance_id=...&log_key=...
func (h *ServerHandler) InstanceLogStream(w http.ResponseWriter, r *http.Request) {
	instID := r.URL.Query().Get("instance_id")
	logKey := r.URL.Query().Get("log_key")

	// 1. 查找信息
	inst, ok := h.instMgr.GetInstance(instID)
	if !ok {
		http.Error(w, "Instance not found", 404)
		return
	}

	node, exists := h.nodeMgr.GetNode(inst.NodeIP)
	if !exists {
		http.Error(w, "Node offline", 404)
		return
	}

	// 2. 构造 Worker WS URL
	// 格式: ws://IP:Port/api/log/ws...
	workerWsURL := fmt.Sprintf("ws://%s:%d/api/log/ws?instance_id=%s&log_key=%s",
		node.IP, node.Port, instID, url.QueryEscape(logKey))

	log.Printf("[LogProxy] Connecting to Worker: %s", workerWsURL)

	// 3. Dial Worker (Master 作为客户端连接 Worker)
	workerConn, _, err := websocket.DefaultDialer.Dial(workerWsURL, nil)
	if err != nil {
		log.Printf("[LogProxy] Dial failed: %v", err)
		http.Error(w, fmt.Sprintf("Connect worker failed: %v", err), 502)
		return
	}
	defer workerConn.Close()

	// 4. Upgrade Frontend (Master 作为服务端响应前端)
	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[LogProxy] Upgrade failed: %v", err)
		return
	}
	defer clientConn.Close()

	// 5. 双向管道 (Pipe)
	errChan := make(chan error, 2)

	// Worker -> Frontend (日志流主要方向)
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

	// Frontend -> Worker (处理关闭信号等)
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

	// 阻塞直到任意一方断开
	<-errChan
}
