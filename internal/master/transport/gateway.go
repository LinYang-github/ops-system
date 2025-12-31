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
	SendChan chan *protocol.WSMessage
	NodeID   string
}

// WorkerGateway 管理所有 Worker 连接
type WorkerGateway struct {
	nodeMgr *manager.NodeManager
	cfgMgr  *manager.ConfigManager
	instMgr *manager.InstanceManager

	// Key: NodeID (UUID), Value: *WorkerConnection
	conns sync.Map

	// [新增] 同步请求等待通道 Key: RequestID, Value: chan *protocol.WSMessage
	pendingRequests sync.Map

	// [修改] 统一的隧道会话管理 Key: SessionID, Value: chan *websocket.Conn
	tunnelSessions sync.Map
}

func NewWorkerGateway(nm *manager.NodeManager, cm *manager.ConfigManager, im *manager.InstanceManager) *WorkerGateway {
	return &WorkerGateway{
		nodeMgr: nm,
		cfgMgr:  cm,
		instMgr: im,
	}
}

// HandleConnection 处理 Worker 接入
func (g *WorkerGateway) HandleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[Gateway] Upgrade failed: %v", err)
		return
	}

	wc := &WorkerConnection{
		Conn:     conn,
		SendChan: make(chan *protocol.WSMessage, 128),
	}

	go g.writePump(wc)
	go g.readPump(wc)
}

// readPump 读取 Worker 发来的数据
func (g *WorkerGateway) readPump(wc *WorkerConnection) {
	var identifiedID string
	defer func() {
		wc.Conn.Close()
		if identifiedID != "" {
			g.conns.Delete(identifiedID)
			log.Printf("[Gateway] Worker disconnected: %s", identifiedID)
			g.nodeMgr.MarkOffline(identifiedID)
			ws.BroadcastNodes(g.nodeMgr.GetAllNodes())
		}
		close(wc.SendChan)
	}()

	wc.Conn.SetReadLimit(512 * 1024)

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
			var req protocol.RegisterRequest
			if err := json.Unmarshal(msg.Payload, &req); err == nil {
				nodeID := req.Info.ID
				if nodeID == "" {
					return
				}
				identifiedID = nodeID
				wc.NodeID = nodeID
				g.conns.Store(nodeID, wc)
				g.nodeMgr.HandleHeartbeat(req, req.Info.IP)
				ws.BroadcastNodes(g.nodeMgr.GetAllNodes())
				g.sendGlobalConfig(wc)
			}

		case protocol.TypeStatusReport:
			var report protocol.InstanceStatusReport
			if err := json.Unmarshal(msg.Payload, &report); err == nil && g.instMgr != nil {
				g.instMgr.UpdateInstanceFullStatus(&report)
			}

		case protocol.TypeResponse:
			// 处理 RPC 响应
			if ch, ok := g.pendingRequests.Load(msg.Id); ok {
				select {
				case ch.(chan *protocol.WSMessage) <- &msg:
				default:
				}
			}
		}
	}
}

// writePump 负责写数据
func (g *WorkerGateway) writePump(wc *WorkerConnection) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-wc.SendChan:
			if !ok {
				wc.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			wc.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := wc.Conn.WriteJSON(msg); err != nil {
				return
			}
		case <-ticker.C:
			wc.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := wc.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// SendCommand 异步下发指令
func (g *WorkerGateway) SendCommand(nodeID string, cmd interface{}) error {
	val, ok := g.conns.Load(nodeID)
	if !ok {
		return fmt.Errorf("worker %s offline", nodeID)
	}
	wc := val.(*WorkerConnection)
	msg, _ := protocol.NewWSMessage(protocol.TypeCommand, "cmd-"+uuid.NewString(), cmd)

	select {
	case wc.SendChan <- msg:
		return nil
	default:
		return fmt.Errorf("send buffer full")
	}
}

// SyncCall 同步调用 Worker (RPC)
func (g *WorkerGateway) SyncCall(nodeID string, msgType string, reqPayload interface{}, respPayload interface{}, timeout time.Duration) error {
	val, ok := g.conns.Load(nodeID)
	if !ok {
		return fmt.Errorf("worker %s offline", nodeID)
	}
	wc := val.(*WorkerConnection)

	reqID := uuid.NewString()
	respChan := make(chan *protocol.WSMessage, 1)
	g.pendingRequests.Store(reqID, respChan)
	defer g.pendingRequests.Delete(reqID)

	reqMsg, err := protocol.NewWSMessage(msgType, reqID, reqPayload)
	if err != nil {
		return err
	}

	select {
	case wc.SendChan <- reqMsg:
	default:
		return fmt.Errorf("send buffer full")
	}

	select {
	case respMsg := <-respChan:
		if err := json.Unmarshal(respMsg.Payload, respPayload); err != nil {
			return fmt.Errorf("decode response failed: %v", err)
		}
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("request timeout")
	}
}

// IsConnected 检查在线状态
func (g *WorkerGateway) IsConnected(nodeID string) bool {
	_, ok := g.conns.Load(nodeID)
	return ok
}

// sendGlobalConfig 下发配置
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
// 隧道 (Tunnel) 逻辑
// ----------------------------------------------------------------------------

// AwaitTunnelConnection 等待 Worker 反向连接
func (g *WorkerGateway) AwaitTunnelConnection(sessionID string, timeout time.Duration) (*websocket.Conn, error) {
	ch := make(chan *websocket.Conn, 1)
	g.tunnelSessions.Store(sessionID, ch)
	defer g.tunnelSessions.Delete(sessionID)

	select {
	case conn := <-ch:
		return conn, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("wait for worker tunnel timeout")
	}
}

// HandleTunnel 处理 Worker 的隧道连接请求
// Route: /api/worker/tunnel
func (g *WorkerGateway) HandleTunnel(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		http.Error(w, "missing session_id", 400)
		return
	}

	val, ok := g.tunnelSessions.Load(sessionID)
	if !ok {
		http.Error(w, "invalid or expired session", 403)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[Gateway] Tunnel upgrade failed: %v", err)
		return
	}

	// 移交连接
	ch := val.(chan *websocket.Conn)
	select {
	case ch <- conn:
		// 成功
	default:
		conn.Close()
	}
}

// RequestTunnel 请求 Worker 建立隧道
func (g *WorkerGateway) RequestTunnel(nodeID string, req protocol.TunnelStartRequest) error {
	// 使用特殊 Payload 格式，Worker Client 会解析 action="start_tunnel"
	payload := map[string]interface{}{
		"action":      "start_tunnel",
		"session_id":  req.SessionID,
		"type":        req.Type,
		"instance_id": req.InstanceID,
		"log_key":     req.LogKey,
	}
	return g.SendCommand(nodeID, payload)
}
