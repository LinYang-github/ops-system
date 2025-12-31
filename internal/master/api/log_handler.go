package api

import (
	"encoding/json"
	"net/http"
	"time"

	"ops-system/pkg/code"
	"ops-system/pkg/e"
	"ops-system/pkg/protocol"
	"ops-system/pkg/response"

	"github.com/google/uuid"
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

	// 1. 获取实例
	inst, ok := h.instMgr.GetInstance(instID)
	if !ok {
		response.Error(w, e.New(code.InstanceNotFound, "实例不存在", nil))
		return
	}

	// 2. 检查节点连接
	if !h.gateway.IsConnected(inst.NodeID) {
		response.Error(w, e.New(code.NodeOffline, "节点离线", nil))
		return
	}

	// 3. [修复] 使用 WebSocket 同步调用 Worker
	reqData := protocol.LogFilesRequest{InstanceID: instID}
	var respData protocol.LogFilesResp

	// 调用 Gateway 等待结果 (5秒超时)
	err := h.gateway.SyncCall(inst.NodeID, protocol.TypeLogFiles, reqData, &respData, 5*time.Second)

	if err != nil {
		response.Error(w, e.New(code.NetworkError, "获取文件列表失败: "+err.Error(), err))
		return
	}

	// 检查业务错误
	if respData.Error != "" {
		response.Error(w, e.New(code.ServerError, respData.Error, nil))
		return
	}

	response.Success(w, respData)
}

const (
	// 允许写入消息的最大等待时间
	writeWait = 10 * time.Second

	// 允许读取 Pong 消息的最大等待时间
	pongWait = 60 * time.Second

	// 发送 Ping 的周期 (必须小于 pongWait)
	pingPeriod = (pongWait * 9) / 10
)

// handleInstanceLogStream 代理日志 WebSocket (反向隧道版)
func (h *ServerHandler) InstanceLogStream(w http.ResponseWriter, r *http.Request) {
	instID := r.URL.Query().Get("instance_id")
	logKey := r.URL.Query().Get("log_key")

	inst, ok := h.instMgr.GetInstance(instID)
	if !ok {
		http.Error(w, "Instance not found", 404)
		return
	}

	// 1. 检查控制通道连接
	if !h.gateway.IsConnected(inst.NodeID) {
		http.Error(w, "Node offline (Control channel disconnected)", 404)
		return
	}

	// 2. 准备会话
	sessionID := uuid.NewString()

	// 3. 发送指令通知 Worker 反向连接
	err := h.gateway.RequestTunnel(inst.NodeID, protocol.TunnelStartRequest{
		SessionID:  sessionID,
		Type:       "log",
		InstanceID: instID,
		LogKey:     logKey,
	})
	if err != nil {
		http.Error(w, "Failed to request tunnel: "+err.Error(), 500)
		return
	}

	// 4. 等待 Worker 连接 (最多 10秒)
	workerConn, err := h.gateway.AwaitTunnelConnection(sessionID, 10*time.Second)
	if err != nil {
		http.Error(w, err.Error(), 504)
		return
	}
	defer workerConn.Close()

	// 5. 升级前端连接
	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer clientConn.Close()

	// 6. 双向管道 (Bridge)
	// 使用 errChan 捕获任意一方的断开
	errChan := make(chan error, 2)

	// Worker (Log Source) -> Frontend
	go func() {
		for {
			mt, msg, err := workerConn.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}
			if err := clientConn.WriteMessage(mt, msg); err != nil {
				errChan <- err
				return
			}
		}
	}()

	// Frontend (Control) -> Worker (e.g. Ping/Close)
	go func() {
		for {
			mt, msg, err := clientConn.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}
			if err := workerConn.WriteMessage(mt, msg); err != nil {
				errChan <- err
				return
			}
		}
	}()

	<-errChan
	// 退出时 defer 会关闭两个连接
}
