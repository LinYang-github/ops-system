package transport

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"ops-system/internal/worker/agent"
	"ops-system/internal/worker/executor"
	"ops-system/internal/worker/handler"
	"ops-system/internal/worker/utils"
	"ops-system/pkg/protocol"

	"github.com/gorilla/websocket"
)

// GlobalClient 全局实例 (仅供 main.go 初始化或调试使用，内部逻辑不依赖它)
var GlobalClient *WorkerClient

type WorkerClient struct {
	MasterURL        string
	Secret           string
	Conn             *websocket.Conn
	SendChan         chan *protocol.WSMessage
	updateTickerChan chan time.Duration

	// [核心] 依赖注入 Executor Manager
	execMgr *executor.Manager
}

// StartClient 启动 WebSocket 客户端
// 必须传入 executor.Manager 实例
func StartClient(masterURL, secret string, execMgr *executor.Manager) *WorkerClient {
	client := &WorkerClient{
		MasterURL:        masterURL,
		Secret:           secret,
		SendChan:         make(chan *protocol.WSMessage, 64),
		updateTickerChan: make(chan time.Duration, 1),
		execMgr:          execMgr,
	}

	GlobalClient = client // 赋值给全局变量以兼容部分遗留逻辑(可选)

	go client.connectLoop()
	return client
}

func (c *WorkerClient) connectLoop() {
	for {
		// 1. 构造 WebSocket URL
		u, err := url.Parse(c.MasterURL)
		if err != nil {
			log.Printf("❌ Fatal: Invalid Master URL config: %v", err)
			return
		}

		switch u.Scheme {
		case "https":
			u.Scheme = "wss"
		case "http":
			u.Scheme = "ws"
		default:
			u.Scheme = "ws"
		}

		u.Path = "/api/worker/ws"
		wsURL := u.String()

		header := http.Header{}
		header.Set("Authorization", "Bearer "+c.Secret)

		dialer := websocket.DefaultDialer
		dialer.HandshakeTimeout = 5 * time.Second

		conn, _, err := dialer.Dial(wsURL, header)
		if err != nil {
			log.Printf("⚠️ Connect failed: %v. Retry in 5s...", err)
			time.Sleep(5 * time.Second)
			continue
		}

		// 创建生命周期控制通道
		stopChan := make(chan struct{})

		c.Conn = conn
		log.Printf("✅ WebSocket Connected!")

		// 连接成功后发送注册包
		c.sendPacket(protocol.TypeRegister)

		// 启动心跳和发送循环
		go c.heartbeatLoop(stopChan)
		go c.writePump(conn, stopChan)

		// 阻塞读取
		c.readLoop()

		// 断开处理
		close(stopChan) // 通知子协程退出
		c.Conn = nil
		log.Printf("❌ Disconnected. Reconnecting...")
		time.Sleep(2 * time.Second)
	}
}

func (c *WorkerClient) readLoop() {
	for {
		if c.Conn == nil {
			return
		}

		_, bytes, err := c.Conn.ReadMessage()
		if err != nil {
			log.Printf("Read error: %v", err)
			return
		}

		var msg protocol.WSMessage
		if err := json.Unmarshal(bytes, &msg); err != nil {
			log.Printf("JSON parse error: %v", err)
			continue
		}

		switch msg.Type {
		case protocol.TypeCommand:
			c.handleCommand(msg)
		case protocol.TypeConfig:
			c.handleConfig(msg)
		case protocol.TypeLogFiles:
			c.handleLogFiles(msg)
		case protocol.TypeCleanupCache:
			c.handleCleanupCache(msg)
		case protocol.TypeScanOrphans:
			c.handleScanOrphans(msg)
		case protocol.TypeDeleteOrphans:
			c.handleDeleteOrphans(msg)
		case protocol.TypeWakeOnLan:
			c.handleWakeOnLan(msg)
		}
	}
}

func (c *WorkerClient) heartbeatLoop(stopChan chan struct{}) {
	interval := 5 * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			select {
			case <-stopChan:
				return
			default:
				if c.Conn != nil {
					c.sendPacket(protocol.TypeHeartbeat)
				}
			}
		case newInterval := <-c.updateTickerChan:
			if newInterval != interval && newInterval > 0 {
				log.Printf("🔄 Updating heartbeat interval: %v -> %v", interval, newInterval)
				interval = newInterval
				ticker.Reset(interval)
			}
		}
	}
}

func (c *WorkerClient) writePump(conn *websocket.Conn, stopChan chan struct{}) {
	ticker := time.NewTicker(20 * time.Second) // Ping interval
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			return
		case msg := <-c.SendChan:
			select {
			case <-stopChan:
				return
			default:
			}
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteJSON(msg); err != nil {
				log.Printf("WS Write Error: %v", err)
				conn.Close()
				return
			}
		case <-ticker.C:
			select {
			case <-stopChan:
				return
			default:
			}
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// -------------------------------------------------------
// 消息处理逻辑 (使用 c.execMgr)
// -------------------------------------------------------

func (c *WorkerClient) handleCommand(msg protocol.WSMessage) {
	// 1. 检查是否为隧道请求
	var rawMap map[string]interface{}
	if err := json.Unmarshal(msg.Payload, &rawMap); err == nil {
		if action, ok := rawMap["action"].(string); ok && action == "start_tunnel" {
			sessionID := rawMap["session_id"].(string)
			tunnelType := rawMap["type"].(string)
			go c.establishTunnel(sessionID, tunnelType, rawMap)
			return
		}
	}

	// 2. 实例操作 (Start/Stop)
	var actionReq protocol.InstanceActionRequest
	if err := json.Unmarshal(msg.Payload, &actionReq); err == nil && actionReq.Action != "" {
		log.Printf("执行实例操作: %s -> %s", actionReq.InstanceID, actionReq.Action)
		if err := c.execMgr.HandleAction(actionReq); err != nil {
			log.Printf("操作失败: %v", err)
		}
		return
	}

	// 3. 部署请求
	var deployReq protocol.DeployRequest
	if err := json.Unmarshal(msg.Payload, &deployReq); err == nil && deployReq.DownloadURL != "" {
		log.Printf("开始异步部署: %s", deployReq.ServiceName)
		go func() {
			c.execMgr.ReportStatus(deployReq.InstanceID, "deploying", 0, 0)
			if err := c.execMgr.DeployInstance(deployReq); err != nil {
				c.execMgr.ReportStatus(deployReq.InstanceID, "error", 0, 0)
			} else {
				c.execMgr.ReportStatus(deployReq.InstanceID, "stopped", 0, 0)
			}
		}()
		return
	}
}

func (c *WorkerClient) handleConfig(msg protocol.WSMessage) {
	var cfg protocol.HeartbeatResponse
	if err := json.Unmarshal(msg.Payload, &cfg); err != nil {
		return
	}
	if cfg.HeartbeatInterval > 0 {
		c.updateTickerChan <- time.Duration(cfg.HeartbeatInterval) * time.Second
	}
	if cfg.MonitorInterval > 0 {
		// 使用注入的 execMgr
		c.execMgr.UpdateMonitorInterval(cfg.MonitorInterval)
	}
}

func (c *WorkerClient) handleLogFiles(msg protocol.WSMessage) {
	var req protocol.LogFilesRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		c.sendErrorResponse(msg.Id, "invalid payload")
		return
	}

	// 使用注入的 execMgr
	files, err := c.execMgr.GetLogFiles(req.InstanceID)

	resp := protocol.LogFilesResp{
		InstanceID: req.InstanceID,
		Files:      files,
	}
	if err != nil {
		resp.Error = err.Error()
	}

	respMsg, _ := protocol.NewWSMessage(protocol.TypeResponse, msg.Id, resp)
	select {
	case c.SendChan <- respMsg:
	default:
	}
}

func (c *WorkerClient) handleCleanupCache(msg protocol.WSMessage) {
	var req protocol.CleanupCacheRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		c.sendErrorResponse(msg.Id, "invalid payload")
		return
	}

	result, err := c.execMgr.CleanupPackageCache(req.Retain)

	resp := protocol.CleanupCacheResponse{
		FreedBytes:   result.FreedBytes,
		DeletedFiles: result.DeletedFiles,
	}
	if err != nil {
		resp.Error = err.Error()
	}

	respMsg, _ := protocol.NewWSMessage(protocol.TypeResponse, msg.Id, resp)
	c.SendChan <- respMsg
}

func (c *WorkerClient) handleScanOrphans(msg protocol.WSMessage) {
	var req protocol.OrphanScanRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		c.sendErrorResponse(msg.Id, "invalid payload")
		return
	}

	sysMap := make(map[string]bool)
	for _, s := range req.ValidSystems {
		sysMap[s] = true
	}
	instMap := make(map[string]bool)
	for _, i := range req.ValidInstances {
		instMap[i] = true
	}

	items, err := c.execMgr.ScanOrphans(sysMap, instMap)

	resp := protocol.OrphanScanNodeResponse{Items: items}
	if err != nil {
		resp.Error = err.Error()
	}

	respMsg, _ := protocol.NewWSMessage(protocol.TypeResponse, msg.Id, resp)
	c.SendChan <- respMsg
}

func (c *WorkerClient) handleDeleteOrphans(msg protocol.WSMessage) {
	var req protocol.OrphanDeleteRequestWorker
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		c.sendErrorResponse(msg.Id, "invalid payload")
		return
	}

	count, _ := c.execMgr.DeleteOrphans(req.Items)

	resp := protocol.OrphanDeleteResponse{DeletedCount: count}
	respMsg, _ := protocol.NewWSMessage(protocol.TypeResponse, msg.Id, resp)
	c.SendChan <- respMsg
}

// -------------------------------------------------------
// 隧道 & 辅助
// -------------------------------------------------------

func (c *WorkerClient) establishTunnel(sessionID, tunnelType string, params map[string]interface{}) {
	u, _ := url.Parse(c.MasterURL)
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	}
	tunnelURL := fmt.Sprintf("%s/api/worker/tunnel?session_id=%s", u.String(), sessionID)

	log.Printf("🚇 Establishing tunnel (%s): %s", tunnelType, tunnelURL)

	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.Secret)

	conn, _, err := websocket.DefaultDialer.Dial(tunnelURL, header)
	if err != nil {
		log.Printf("❌ Tunnel dial failed: %v", err)
		return
	}
	// 注意：连接移交给 Handler 处理，Handler 负责 Close

	if tunnelType == "log" {
		instanceID, _ := params["instance_id"].(string)
		logKey, _ := params["log_key"].(string)
		// 【注意】这里传入 c.execMgr，因为 Handler 需要它来解析路径
		handler.ServeLogStream(conn, instanceID, logKey, c.execMgr)
	} else if tunnelType == "terminal" {
		handler.ServeTerminal(conn)
	} else {
		conn.Close()
	}
}

func (c *WorkerClient) SendStatusReport(report protocol.InstanceStatusReport) {
	if c == nil || c.Conn == nil {
		return
	}
	msg, _ := protocol.NewWSMessage(protocol.TypeStatusReport, "", report)
	select {
	case c.SendChan <- msg:
	default:
	}
}

func (c *WorkerClient) sendErrorResponse(reqID string, errMsg string) {
	// 这里复用 LogFilesResp 的 Error 字段作为通用错误返回
	// 实际项目中最好有通用的 ErrorResponse 结构
	resp := protocol.LogFilesResp{Error: errMsg}
	msg, _ := protocol.NewWSMessage(protocol.TypeResponse, reqID, resp)
	c.SendChan <- msg
}

func (c *WorkerClient) sendPacket(msgType string) {
	// agent 包是无状态的，可以直接调用
	info := agent.GetNodeInfo()
	status := agent.GetStatus()
	req := protocol.RegisterRequest{Info: info, Status: status}

	wsMsg, _ := protocol.NewWSMessage(msgType, "", req)
	select {
	case c.SendChan <- wsMsg:
	default:
	}
}

func (c *WorkerClient) handleWakeOnLan(msg protocol.WSMessage) {
	var req protocol.WakeOnLanRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		log.Printf("❌ [WoL] Invalid payload: %v", err)
		return
	}

	log.Printf("⚡ [WoL] Broadcasting Magic Packet to MAC: %s", req.TargetMAC)
	if err := utils.SendMagicPacket(req.TargetMAC); err != nil {
		log.Printf("❌ [WoL] Failed: %v", err)
	} else {
		log.Printf("✅ [WoL] Packet sent.")
	}
}
