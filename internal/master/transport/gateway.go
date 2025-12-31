package transport

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"ops-system/internal/master/manager"
	"ops-system/internal/master/ws"
	"ops-system/pkg/protocol"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// WorkerConnection 封装单个连接
type WorkerConnection struct {
	Conn     *websocket.Conn
	SendChan chan *protocol.WSMessage // 使用 Channel 解耦发送，避免并发写同一个 Conn
	NodeID   string                   // 节点的唯一 UUID
}

// WorkerGateway 管理所有 Worker 连接
type WorkerGateway struct {
	nodeMgr *manager.NodeManager
	cfgMgr  *manager.ConfigManager
	instMgr *manager.InstanceManager

	// Key: NodeID (UUID), Value: *WorkerConnection
	conns sync.Map

	// 终端会话暂存: SessionID -> Channel (传递 Worker 的连接)
	terminalSessions sync.Map
}

func NewWorkerGateway(nm *manager.NodeManager, cm *manager.ConfigManager, im *manager.InstanceManager) *WorkerGateway {
	return &WorkerGateway{
		nodeMgr: nm,
		cfgMgr:  cm,
		instMgr: im,
	}
}

// HandleConnection 处理 Worker 接入 (在 api/server.go 中注册路由)
func (g *WorkerGateway) HandleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[Gateway] Upgrade failed: %v", err)
		return
	}

	// 封装连接对象
	wc := &WorkerConnection{
		Conn:     conn,
		SendChan: make(chan *protocol.WSMessage, 128), // 缓冲区大小
	}

	// 启动写泵 (WritePump) - 专门负责发送数据
	go g.writePump(wc)

	// 启动读泵 (ReadPump) - 负责接收数据
	go g.readPump(wc)
}

// readPump 读取 Worker 发来的数据
func (g *WorkerGateway) readPump(wc *WorkerConnection) {
	var identifiedID string // 记录当前连接识别出的 NodeID

	defer func() {
		wc.Conn.Close()
		if identifiedID != "" {
			g.conns.Delete(identifiedID)
			log.Printf("[Gateway] Worker disconnected: %s", identifiedID)

			// 立即通知 NodeManager 标记离线，并广播给前端
			// 假设您在 NodeManager 中实现了 MarkOffline 方法
			g.nodeMgr.MarkOffline(identifiedID)
			ws.BroadcastNodes(g.nodeMgr.GetAllNodes())
		}
		close(wc.SendChan) // 关闭通道停止写泵
	}()

	// 设置读取限制
	wc.Conn.SetReadLimit(512 * 1024) // 512KB

	for {
		_, bytes, err := wc.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[Gateway] Read error: %v", err)
			}
			break
		}

		var msg protocol.WSMessage
		if err := json.Unmarshal(bytes, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case protocol.TypeRegister:
			// 处理注册/心跳 (这是连接建立后的第一个包)
			var req protocol.RegisterRequest
			if err := json.Unmarshal(msg.Payload, &req); err == nil {
				nodeID := req.Info.ID
				if nodeID == "" {
					log.Printf("[Gateway] Received register without NodeID from %s", wc.Conn.RemoteAddr())
					return
				}

				// 1. 绑定连接关系
				identifiedID = nodeID
				wc.NodeID = nodeID
				g.conns.Store(nodeID, wc)

				// 2. 更新节点元数据和状态
				g.nodeMgr.HandleHeartbeat(req, req.Info.IP)

				// 3. 立即触发前端广播
				ws.BroadcastNodes(g.nodeMgr.GetAllNodes())

				// 4. 回复动态配置 (告知心跳频率等)
				g.sendGlobalConfig(wc)

				log.Printf("[Gateway] Worker registered: %s (IP: %s)", nodeID, req.Info.IP)
			}

		case protocol.TypeStatusReport:
			// 处理实例状态上报 (CPU/MEM/STATUS)
			var report protocol.InstanceStatusReport
			if err := json.Unmarshal(msg.Payload, &report); err == nil {
				if g.instMgr != nil {
					g.instMgr.UpdateInstanceFullStatus(&report)
				}
			}

		case protocol.TypeResponse:
			// 处理指令执行结果的异步回调 (如果需要)
			log.Printf("[Gateway] Received response from %s: %s", identifiedID, string(msg.Payload))
		}
	}
}

// writePump 专门负责写数据，确保并发安全并处理超时
func (g *WorkerGateway) writePump(wc *WorkerConnection) {
	ticker := time.NewTicker(30 * time.Second) // 辅助 Ping
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-wc.SendChan:
			if !ok {
				// SendChan 已关闭
				wc.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			wc.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := wc.Conn.WriteJSON(msg); err != nil {
				log.Printf("[Gateway] Write error to %s: %v", wc.NodeID, err)
				return
			}

		case <-ticker.C:
			// 发送 Ping 保持连接
			wc.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := wc.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// SendCommand 供 API Handler 调用
// nodeID: 节点的 UUID
// cmd: 指令内容对象 (protocol.InstanceActionRequest 等)
func (g *WorkerGateway) SendCommand(nodeID string, cmd interface{}) error {
	val, ok := g.conns.Load(nodeID)
	if !ok {
		return fmt.Errorf("worker %s is offline or connection not found", nodeID)
	}
	wc := val.(*WorkerConnection)

	// 封装统一的 WS 消息信封
	msg, _ := protocol.NewWSMessage(protocol.TypeCommand, "cmd-"+uuid.NewString(), cmd)

	// 非阻塞发送到写泵
	select {
	case wc.SendChan <- msg:
		return nil
	default:
		return fmt.Errorf("worker %s send buffer full", nodeID)
	}
}

// IsConnected 检查指定 UUID 的 Worker 是否在线
func (g *WorkerGateway) IsConnected(nodeID string) bool {
	_, ok := g.conns.Load(nodeID)
	return ok
}

// sendGlobalConfig 发送全局配置给 Worker
func (g *WorkerGateway) sendGlobalConfig(wc *WorkerConnection) {
	globalCfg, _ := g.cfgMgr.GetGlobalConfig()

	resp := protocol.HeartbeatResponse{
		Code:              200,
		HeartbeatInterval: int64(globalCfg.Worker.HeartbeatInterval),
		MonitorInterval:   int64(globalCfg.Worker.MonitorInterval),
	}

	wsMsg, _ := protocol.NewWSMessage(protocol.TypeConfig, "", resp)
	select {
	case wc.SendChan <- wsMsg:
	default:
	}
}

// ----------------------------------------------------------------------------
// 反向终端 (Terminal Relay) 逻辑
// ----------------------------------------------------------------------------

func (g *WorkerGateway) AwaitWorkerConnection(sessionID string) chan *websocket.Conn {
	ch := make(chan *websocket.Conn, 1)
	g.terminalSessions.Store(sessionID, ch)

	// 15秒超时自动清理，防止内存泄露
	go func() {
		time.Sleep(15 * time.Second)
		if val, loaded := g.terminalSessions.LoadAndDelete(sessionID); loaded {
			close(val.(chan *websocket.Conn))
		}
	}()

	return ch
}

func (g *WorkerGateway) HandleTerminalRelay(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	val, ok := g.terminalSessions.LoadAndDelete(sessionID)
	if !ok {
		http.Error(w, "Session expired or invalid", 400)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[Relay] Upgrade failed: %v", err)
		return
	}

	ch := val.(chan *websocket.Conn)
	select {
	case ch <- conn:
		// 成功移交连接
	default:
		conn.Close()
	}
}
