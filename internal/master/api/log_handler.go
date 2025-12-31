package api

import (
	"encoding/json"
	"net/http"
	"sync"
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

// handleInstanceLogStream 代理日志 WebSocket (反向隧道版 - 修复资源泄漏)
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

	// 5. 升级前端连接
	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		workerConn.Close() // 如果前端升级失败，必须关闭已建立的 Worker 连接
		return
	}

	// =================================================================
	// 修复核心：双向管道 + 截止时间控制 + 统一关闭
	// =================================================================

	// 统一关闭控制器，确保只关闭一次
	var once sync.Once
	closeAll := func() {
		once.Do(func() {
			workerConn.Close()
			clientConn.Close()
		})
	}

	// 退出信号
	done := make(chan struct{})
	defer closeAll()

	// --- 设置 Worker 连接的读超时与 Pong 处理 ---
	workerConn.SetReadLimit(512 * 1024) // 限制单条日志大小
	workerConn.SetReadDeadline(time.Now().Add(pongWait))
	workerConn.SetPongHandler(func(string) error {
		workerConn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// --- 设置 Client 连接的读超时与 Pong 处理 ---
	clientConn.SetReadLimit(512 * 1024)
	clientConn.SetReadDeadline(time.Now().Add(pongWait))
	clientConn.SetPongHandler(func(string) error {
		clientConn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// 协程 A: Worker (Log Source) -> Frontend
	go func() {
		defer close(done) // 任何一方退出，通知主流程
		defer closeAll()

		for {
			// [读] 阻塞读取，依赖 SetReadDeadline 防止僵死
			mt, msg, err := workerConn.ReadMessage()
			if err != nil {
				// log.Printf("Worker read error: %v", err)
				return
			}

			// [写] 设置写超时，防止前端卡死导致 Master 协程泄漏
			clientConn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := clientConn.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}()

	// 协程 B: Frontend (Control) -> Worker
	go func() {
		defer closeAll()
		for {
			mt, msg, err := clientConn.ReadMessage()
			if err != nil {
				return
			}
			workerConn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := workerConn.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}()

	// 协程 C: 心跳保活 (Ping Loop)
	// 主动向两端发送 Ping，触发对方回复 Pong，从而刷新 ReadDeadline
	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				// Ping Frontend
				clientConn.SetWriteDeadline(time.Now().Add(writeWait))
				if err := clientConn.WriteMessage(websocket.PingMessage, nil); err != nil {
					closeAll()
					return
				}
				// Ping Worker
				workerConn.SetWriteDeadline(time.Now().Add(writeWait))
				if err := workerConn.WriteMessage(websocket.PingMessage, nil); err != nil {
					closeAll()
					return
				}
			}
		}
	}()

	// 主协程阻塞等待
	<-done
}
