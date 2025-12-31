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
	SendChan chan *protocol.WSMessage //这也是关键：使用 Channel 解耦发送
	NodeIP   string
}

// WorkerGateway 管理所有 Worker 连接
type WorkerGateway struct {
	nodeMgr *manager.NodeManager
	cfgMgr  *manager.ConfigManager
	// Key: NodeIP, Value: Connection
	conns sync.Map
	// 终端会话暂存: SessionID -> Channel (传递 Worker 的连接)
	terminalSessions sync.Map
}

func NewWorkerGateway(nm *manager.NodeManager, cm *manager.ConfigManager) *WorkerGateway {
	return &WorkerGateway{
		nodeMgr: nm,
		cfgMgr:  cm,
	}
}

// HandleConnection 处理 Worker 接入 (在 api/server.go 中注册路由)
func (g *WorkerGateway) HandleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	// 封装连接对象
	wc := &WorkerConnection{
		Conn:     conn,
		SendChan: make(chan *protocol.WSMessage, 64), // 缓冲区
	}

	// 启动写泵 (WritePump) - 专门负责发送数据，避免锁竞争
	go g.writePump(wc)

	// 启动读泵 (ReadPump) - 负责接收数据
	go g.readPump(wc)
}

// readPump 读取 Worker 发来的数据
func (g *WorkerGateway) readPump(wc *WorkerConnection) {
	defer func() {
		wc.Conn.Close()
		if wc.NodeIP != "" {
			g.conns.Delete(wc.NodeIP)
			log.Printf("Worker disconnected: %s", wc.NodeIP)

			// [新增] 节点断开时，也广播一次，让前端看到状态变为 offline (需 NodeManager 配合)
			// 这里简单触发一次全量更新即可
			ws.BroadcastNodes(g.nodeMgr.GetAllNodes())
		}
	}()

	for {
		_, bytes, err := wc.Conn.ReadMessage()
		if err != nil {
			break
		}

		var msg protocol.WSMessage
		if err := json.Unmarshal(bytes, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case protocol.TypeRegister:
			// 处理注册/心跳
			var req protocol.RegisterRequest
			if err := json.Unmarshal(msg.Payload, &req); err == nil {
				// 1. 绑定连接关系
				wc.NodeIP = req.Info.IP
				g.conns.Store(wc.NodeIP, wc)

				// 2. 更新节点状态
				g.nodeMgr.HandleHeartbeat(req, req.Info.IP)

				// 3. [关键修复] 触发前端广播
				// 通知 WebSocket Hub 数据已更新，Hub 会在下一秒统一推送给前端
				ws.BroadcastNodes(g.nodeMgr.GetAllNodes())

				// 2. [新增] 回复动态配置
				g.sendGlobalConfig(wc)
			}
		case protocol.TypeResponse:
			log.Printf("Received response from %s: %s", wc.NodeIP, string(msg.Payload))
		}
	}
}

// writePump 专门负责写数据，确保并发安全
func (g *WorkerGateway) writePump(wc *WorkerConnection) {
	for msg := range wc.SendChan {
		wc.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := wc.Conn.WriteJSON(msg); err != nil {
			log.Printf("Write error to %s: %v", wc.NodeIP, err)
			wc.Conn.Close()
			return
		}
	}
}

// SendCommand 供 API Handler 调用
func (g *WorkerGateway) SendCommand(nodeIP string, cmd interface{}) error {
	val, ok := g.conns.Load(nodeIP)
	if !ok {
		return fmt.Errorf("worker offline")
	}
	wc := val.(*WorkerConnection)

	// 封装消息
	msg, _ := protocol.NewWSMessage(protocol.TypeCommand, "cmd-"+uuid.NewString(), cmd)

	// 非阻塞发送 (如果 Worker 卡死，不应该阻塞 Master)
	select {
	case wc.SendChan <- msg:
		return nil
	default:
		return fmt.Errorf("worker send buffer full")
	}
}

// IsConnected 检查指定 IP 的 Worker 是否已建立 WebSocket 连接
func (g *WorkerGateway) IsConnected(nodeIP string) bool {
	_, ok := g.conns.Load(nodeIP)
	return ok
}

// [新增] 发送全局配置给 Worker
func (g *WorkerGateway) sendGlobalConfig(wc *WorkerConnection) {
	// 获取配置 (如果 DB 没配置则使用默认值)
	globalCfg, _ := g.cfgMgr.GetGlobalConfig()

	// 构造响应包 (复用 HeartbeatResponse 结构)
	resp := protocol.HeartbeatResponse{
		Code:              200,
		HeartbeatInterval: int64(globalCfg.Worker.HeartbeatInterval),
		MonitorInterval:   int64(globalCfg.Worker.MonitorInterval),
	}

	wsMsg, _ := protocol.NewWSMessage(protocol.TypeConfig, "", resp)

	// 非阻塞发送
	select {
	case wc.SendChan <- wsMsg:
	default:
	}
}

// [新增] 等待 Worker 反向连接 (供前端 Handler 调用)
// 返回一个 channel，Worker 连上来后会把 Conn 发进去
func (g *WorkerGateway) AwaitWorkerConnection(sessionID string) chan *websocket.Conn {
	ch := make(chan *websocket.Conn, 1)
	g.terminalSessions.Store(sessionID, ch)

	// 超时清理机制
	go func() {
		time.Sleep(10 * time.Second)
		g.terminalSessions.Delete(sessionID)
		close(ch) // 如果超时没人连，关闭 channel
	}()

	return ch
}

// [新增] 处理 Worker 的反向终端接入 (供 Worker 调用)
// URL: /api/worker/terminal/relay?session_id=...
func (g *WorkerGateway) HandleTerminalRelay(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	val, ok := g.terminalSessions.Load(sessionID)
	if !ok {
		http.Error(w, "Session expired or invalid", 400)
		return
	}

	// 升级连接
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	// 将 Worker 的连接传递给正在等待的主线程 (NodeHandler)
	ch := val.(chan *websocket.Conn)

	// 防止向已关闭的 channel 发送
	defer func() { recover() }()

	select {
	case ch <- conn:
		// 发送成功，Handler 接管连接，这里不需要 Close
	default:
		conn.Close()
	}

	// 任务完成，清理 Map
	g.terminalSessions.Delete(sessionID)
}
