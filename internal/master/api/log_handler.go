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

const (
	// 允许写入消息的最大等待时间
	writeWait = 10 * time.Second

	// 允许读取 Pong 消息的最大等待时间
	pongWait = 60 * time.Second

	// 发送 Ping 的周期 (必须小于 pongWait)
	pingPeriod = (pongWait * 9) / 10
)

// handleInstanceLogStream 代理日志 WebSocket
func (h *ServerHandler) InstanceLogStream(w http.ResponseWriter, r *http.Request) {
	instID := r.URL.Query().Get("instance_id")
	logKey := r.URL.Query().Get("log_key")

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

	workerWsURL := fmt.Sprintf("ws://%s:%d/api/log/ws?instance_id=%s&log_key=%s",
		node.IP, node.Port, instID, url.QueryEscape(logKey))

	// 1. Dial Worker
	workerConn, _, err := websocket.DefaultDialer.Dial(workerWsURL, nil)
	if err != nil {
		log.Printf("[LogProxy] Dial failed: %v", err)
		http.Error(w, fmt.Sprintf("Connect worker failed: %v", err), 502)
		return
	}
	defer workerConn.Close()

	// 2. Upgrade Frontend
	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[LogProxy] Upgrade failed: %v", err)
		return
	}
	defer clientConn.Close()

	// 3. 设置清理通道
	// 使用 buffered channel 防止 goroutine 泄露
	errChan := make(chan error, 2)

	// =====================================
	// 管道 A: Worker -> Frontend (主要日志流)
	// =====================================
	go func() {
		defer func() {
			// 确保退出时写入 error 以触发主程退出
			// recover 防止向已关闭的 channel 写入 (虽然 buffer=2 基本不会发生)
			recover()
		}()

		for {
			// 读取 Worker 数据
			// Worker 端负责发 Ping，这里不需要 SetReadDeadline，
			// 或者可以设置一个较长的 ReadDeadline 防止 Worker 假死
			mt, message, err := workerConn.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}

			// 写入 Frontend
			// 【关键修复】设置写入超时，防止前端网络卡死导致 Master 协程堆积
			clientConn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := clientConn.WriteMessage(mt, message); err != nil {
				errChan <- err
				return
			}
		}
	}()

	// =====================================
	// 管道 B: Frontend -> Worker (心跳/控制流)
	// =====================================
	go func() {
		defer func() { recover() }()

		// 【关键修复】设置读超时 + Pong 处理
		// 如果前端意外断开且没有发 FIN 包，这里会超时退出
		clientConn.SetReadDeadline(time.Now().Add(pongWait))
		clientConn.SetPongHandler(func(string) error {
			clientConn.SetReadDeadline(time.Now().Add(pongWait))
			return nil
		})

		for {
			mt, message, err := clientConn.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}

			// 转发给 Worker
			workerConn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := workerConn.WriteMessage(mt, message); err != nil {
				errChan <- err
				return
			}
		}
	}()

	// =====================================
	// 管道 C: 定时 Ping 前端 (保活)
	// =====================================
	// 增加一个协程专门给前端发 Ping，防止浏览器 WS 断开
	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				clientConn.SetWriteDeadline(time.Now().Add(writeWait))
				if err := clientConn.WriteMessage(websocket.PingMessage, nil); err != nil {
					// 写 Ping 失败，说明连接已断，不需要通过 errChan 通知，
					// 因为上面的 管道B 读操作也会随之报错或超时
					return
				}
			}
		}
	}()

	// 4. 阻塞等待任意一方断开
	// 只要有一个报错，主函数返回，defer 触发 Close，所有协程随之退出
	err = <-errChan
	log.Printf("[LogProxy] Stream closed: %v", err)
}
