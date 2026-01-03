package transport

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"ops-system/internal/master/manager"
	"ops-system/internal/master/ws"
	"ops-system/pkg/config"
	"ops-system/pkg/protocol"
	"ops-system/pkg/utils"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// WorkerConnection 封装单个连接
type WorkerConnection struct {
	Conn       *websocket.Conn
	SendChan   chan *protocol.WSMessage
	NodeID     string
	ClientIP   string
	MasterHost string // [新增] 保存 Master 的访问地址 (Host头)，用于生成下载链接
}

// WorkerGateway 管理所有 Worker 连接
type WorkerGateway struct {
	nodeMgr *manager.NodeManager
	cfgMgr  *manager.ConfigManager
	instMgr *manager.InstanceManager
	sysMgr  *manager.SystemManager

	// Key: NodeID (UUID), Value: *WorkerConnection
	conns sync.Map

	// [新增] 同步请求等待通道 Key: RequestID, Value: chan *protocol.WSMessage
	pendingRequests sync.Map

	// [修改] 统一的隧道会话管理 Key: SessionID, Value: chan *websocket.Conn
	tunnelSessions sync.Map
}

func NewWorkerGateway(nm *manager.NodeManager, cm *manager.ConfigManager, im *manager.InstanceManager, sm *manager.SystemManager) *WorkerGateway {
	return &WorkerGateway{
		nodeMgr: nm,
		cfgMgr:  cm,
		instMgr: im,
		sysMgr:  sm,
	}
}

// HandleConnection 处理 Worker 接入
func (g *WorkerGateway) HandleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[Gateway] Upgrade failed: %v", err)
		return
	}

	// [新增] 获取真实 IP (支持 X-Forwarded-For)
	realIP := utils.GetClientIP(r)

	wc := &WorkerConnection{
		Conn:       conn,
		SendChan:   make(chan *protocol.WSMessage, 128),
		ClientIP:   realIP, // 绑定 IP
		MasterHost: r.Host, // [新增] 捕获当前请求的 Host (例如 192.168.1.100:8080)
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
		case protocol.TypeRegister, protocol.TypeHeartbeat:
			var req protocol.RegisterRequest
			if err := json.Unmarshal(msg.Payload, &req); err == nil {
				nodeID := req.Info.ID
				if nodeID == "" {
					return // 忽略无效包
				}

				// 首次识别
				if identifiedID == "" {
					identifiedID = nodeID
					wc.NodeID = nodeID
					g.conns.Store(nodeID, wc)
					log.Printf("[Gateway] Worker connected: %s (IP: %s)", nodeID, wc.ClientIP)
				}

				// 确定入库显示的 IP
				displayIP := wc.ClientIP
				if (displayIP == "127.0.0.1" || displayIP == "::1") && req.Info.IP != "" && req.Info.IP != "127.0.0.1" {
					displayIP = req.Info.IP
				}

				// 处理心跳更新 (更新 DB 和 Cache)
				g.nodeMgr.HandleHeartbeat(req, displayIP)

				// 广播更新
				ws.BroadcastNodes(g.nodeMgr.GetAllNodes())

				// 仅在 Register 时执行的逻辑
				if msg.Type == protocol.TypeRegister {
					// 1. 下发全局配置
					g.sendGlobalConfig(wc)

					// 2. [新增] 检查版本并自动升级 (Hash 对齐)
					// 异步执行，不阻塞后续心跳处理
					go g.checkAndAutoUpgrade(wc, req.Info)
				}
			}
		case protocol.TypeStatusReport:
			var report protocol.InstanceStatusReport
			if err := json.Unmarshal(msg.Payload, &report); err == nil && g.instMgr != nil {
				g.instMgr.UpdateInstanceFullStatus(&report)
				if g.sysMgr != nil {
					data := g.sysMgr.GetFullView(g.instMgr)
					ws.BroadcastSystems(data)
				}
			}
		case protocol.TypeResponse:
			if ch, ok := g.pendingRequests.Load(msg.Id); ok {
				select {
				case ch.(chan *protocol.WSMessage) <- &msg:
				default:
				}
			}
		case protocol.TypeWakeOnLan:
			// 如果 Worker 具备反向控制能力（如作为跳板唤醒其他节点），逻辑在此扩展
			// 目前主要是 Master 下发给 Worker，这里不需要处理 Worker 发来的 WoL
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

// BroadcastConfig 向所有连接的 Worker 广播最新的全局配置
func (g *WorkerGateway) BroadcastConfig(cfg config.GlobalConfig) {
	// 1. 构造 Worker 能识别的协议包
	// 对应 pkg/protocol/types.go 中的 HeartbeatResponse
	payload := protocol.HeartbeatResponse{
		Code:              200,
		HeartbeatInterval: int64(cfg.Worker.HeartbeatInterval),
		MonitorInterval:   int64(cfg.Worker.MonitorInterval),
	}

	// 2. 封装 WebSocket 消息
	wsMsg, err := protocol.NewWSMessage(protocol.TypeConfig, "", payload)
	if err != nil {
		log.Printf("[Gateway] Failed to create config message: %v", err)
		return
	}

	log.Printf("[Gateway] Broadcasting config update to all workers...")

	// 3. 遍历所有连接并发送
	count := 0
	g.conns.Range(func(key, value interface{}) bool {
		wc := value.(*WorkerConnection)
		select {
		case wc.SendChan <- wsMsg:
			count++
		default:
			log.Printf("[Gateway] Worker %s send buffer full, skipping config update", wc.NodeID)
		}
		return true
	})

	log.Printf("[Gateway] Config broadcasted to %d workers.", count)
}

// SendWakeInstruction 发送唤醒指令
func (g *WorkerGateway) SendWakeInstruction(nodeID string, payload protocol.WakeOnLanRequest) error {
	val, ok := g.conns.Load(nodeID)
	if !ok {
		return fmt.Errorf("proxy node offline")
	}
	wc := val.(*WorkerConnection)

	msg, _ := protocol.NewWSMessage(protocol.TypeWakeOnLan, "", payload)

	select {
	case wc.SendChan <- msg:
		return nil
	default:
		return fmt.Errorf("send buffer full")
	}
}

// SendUpgradeInstruction 发送升级指令
func (g *WorkerGateway) SendUpgradeInstruction(nodeID string, payload protocol.WorkerUpgradeRequest) error {
	val, ok := g.conns.Load(nodeID)
	if !ok {
		return fmt.Errorf("worker offline")
	}
	wc := val.(*WorkerConnection)

	msg, _ := protocol.NewWSMessage(protocol.TypeWorkerUpgrade, "", payload)

	select {
	case wc.SendChan <- msg:
		return nil
	default:
		return fmt.Errorf("send buffer full")
	}
}

// [新增] 检查并自动升级
func (g *WorkerGateway) checkAndAutoUpgrade(wc *WorkerConnection, info protocol.NodeInfo) {
	// 1. 构造配置 Key (如 agent_target_hash_linux_amd64)
	// 需确保 info.OS 和 info.Arch 格式规范，这里做简单处理
	osType := "linux"
	if strings.Contains(strings.ToLower(info.OS), "windows") {
		osType = "windows"
	}
	// 假设暂只支持 amd64，实际可根据 info.Arch 动态拼接
	confKey := fmt.Sprintf("agent_target_hash_%s_amd64", osType)

	// 2. 获取期望 Hash
	targetHash, err := g.cfgMgr.GetSetting(confKey)
	if err != nil || targetHash == "" {
		return // 没有设置期望版本，跳过
	}

	// 3. 比对 Hash (忽略空值防止误判)
	if info.AgentHash != "" && info.AgentHash != targetHash {
		log.Printf("🚀 [AutoUpgrade] Node %s hash mismatch (Curr: %s... vs Target: %s...), triggering upgrade.",
			info.IP, info.AgentHash[:8], targetHash[:8])

		// 4. 构造下载链接 (这里需要获取 Master Host，可以在 Gateway 初始化时传入或配置中获取)
		// 简化起见，假设文件名固定
		fileName := "worker_linux_amd64"
		if osType == "windows" {
			fileName = "worker_windows_amd64.exe"
		}

		// 注意：这里需要获取 Master 的外部访问地址。
		// 生产环境建议在 config.yaml 配置 external_url，或者通过 request context 传递
		// 这里暂时硬编码示例，请替换为你的实际逻辑
		masterAddr := "127.0.0.1:8080" // ⚠️ 需动态获取
		// 如果 ConfigManager 能拿到 MasterConfig 最好

		downloadURL := fmt.Sprintf("http://%s/download/system/%s", masterAddr, fileName)

		payload := protocol.WorkerUpgradeRequest{
			DownloadURL: downloadURL,
			Checksum:    targetHash,
			Version:     "auto-sync",
		}

		// 5. 下发指令 (复用之前的 SendUpgradeInstruction)
		g.SendUpgradeInstruction(info.ID, payload)
	}
}
