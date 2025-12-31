package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"ops-system/internal/master/ws"
	"ops-system/pkg/code"
	"ops-system/pkg/e"
	"ops-system/pkg/protocol"
	"ops-system/pkg/response"
	"ops-system/pkg/utils"

	"github.com/google/uuid"
)

// HandleHeartbeat 处理 Worker 心跳 (HTTP 兼容接口)
// 新版架构中，Worker 主要通过 WebSocket 上报心跳，此接口用于兼容或 fallback
// POST /api/worker/heartbeat
func (h *ServerHandler) HandleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, e.New(code.MethodNotAllowed, "Method not allowed", nil))
		return
	}

	var req protocol.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "Invalid heartbeat payload", err))
		return
	}

	remoteIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(remoteIP); err == nil {
		remoteIP = host
	}
	// 如果是本地回环且请求体带了IP，优先使用请求体的IP
	if (remoteIP == "127.0.0.1" || remoteIP == "::1") && req.Info.IP != "" && req.Info.IP != "127.0.0.1" {
		remoteIP = req.Info.IP
	}

	// 1. 更新数据库/缓存
	h.nodeMgr.HandleHeartbeat(req, remoteIP)

	// 2. 广播给前端
	ws.BroadcastNodes(h.nodeMgr.GetAllNodes())

	// 3. 获取动态配置
	var hbInterval, monInterval int64 = 5, 3
	if globalCfg, err := h.configMgr.GetGlobalConfig(); err == nil {
		hbInterval = int64(globalCfg.Worker.HeartbeatInterval)
		monInterval = int64(globalCfg.Worker.MonitorInterval)
	}

	// 4. 返回标准响应
	resp := protocol.HeartbeatResponse{
		Code:              200,
		HeartbeatInterval: hbInterval,
		MonitorInterval:   monInterval,
	}

	response.Success(w, resp)
}

// ListNodes 获取节点列表
// GET /api/nodes
func (h *ServerHandler) ListNodes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	nodes := h.nodeMgr.GetAllNodes()
	response.Success(w, nodes)
}

// AddNode 添加规划节点
// POST /api/nodes/add
func (h *ServerHandler) AddNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP   string `json:"ip"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	if err := h.nodeMgr.AddPlannedNode(req.IP, req.Name); err != nil {
		response.Error(w, e.New(code.NodeRegisterFailed, "添加节点失败", err))
		return
	}

	h.logMgr.RecordLog(utils.GetClientIP(r), "add_node", "node", req.IP, req.Name, "success")
	ws.BroadcastNodes(h.nodeMgr.GetAllNodes())

	response.Success(w, nil)
}

// DeleteNode 删除节点
// POST /api/nodes/delete
func (h *ServerHandler) DeleteNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	if err := h.nodeMgr.DeleteNode(req.IP); err != nil {
		response.Error(w, e.New(code.DatabaseError, "删除节点失败", err))
		return
	}

	h.logMgr.RecordLog(utils.GetClientIP(r), "delete_node", "node", req.IP, "", "success")
	ws.BroadcastNodes(h.nodeMgr.GetAllNodes())

	response.Success(w, nil)
}

// RenameNode 重命名节点
// POST /api/nodes/rename
func (h *ServerHandler) RenameNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	if err := h.nodeMgr.RenameNode(req.ID, req.Name); err != nil {
		response.Error(w, e.New(code.DatabaseError, "重命名失败", err))
		return
	}

	h.logMgr.RecordLog(utils.GetClientIP(r), "rename_node", "node", req.ID, req.Name, "success")
	ws.BroadcastNodes(h.nodeMgr.GetAllNodes())

	response.Success(w, nil)
}

// ResetNodeName 重置节点名为 Hostname
// POST /api/nodes/reset_name
func (h *ServerHandler) ResetNodeName(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	if err := h.nodeMgr.ResetNodeName(req.ID); err != nil {
		response.Error(w, e.New(code.DatabaseError, "重置名称失败", err))
		return
	}

	h.logMgr.RecordLog(utils.GetClientIP(r), "reset_node_name", "node", req.ID, "", "success")
	ws.BroadcastNodes(h.nodeMgr.GetAllNodes())

	response.Success(w, nil)
}

// TriggerCmd 下发 CMD 指令 (WebSocket 模式)
// POST /api/ctrl/cmd
func (h *ServerHandler) TriggerCmd(w http.ResponseWriter, r *http.Request) {
	type TriggerReq struct {
		TargetIP string `json:"target_ip"`
		Command  string `json:"command"`
	}

	var trigger TriggerReq
	if err := json.NewDecoder(r.Body).Decode(&trigger); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	// 1. 检查节点是否存在且在线
	node, exists := h.nodeMgr.GetNode(trigger.TargetIP)
	if !exists || node.Status != "online" {
		response.Error(w, e.New(code.NodeNotFound, "节点不存在或离线", nil))
		return
	}

	// 2. 检查 WebSocket 连接状态 (通过 Gateway)
	if !h.gateway.IsConnected(trigger.TargetIP) {
		response.Error(w, e.New(code.NodeOffline, "节点 WebSocket 未连接，无法下发指令", nil))
		return
	}

	// 3. 构造请求
	workerReq := protocol.CommandRequest{Command: trigger.Command}

	// 4. 通过 WebSocket 网关发送
	// 注意：此处改为异步下发，无法立即获取 Command 执行结果(stdout)
	// 如果需要结果，需实现 Response 回调机制或查看日志
	err := h.gateway.SendCommand(trigger.TargetIP, workerReq)
	if err != nil {
		h.logMgr.RecordLog(utils.GetClientIP(r), "exec_cmd", "node", trigger.TargetIP, "Send Failed", "fail")
		response.Error(w, e.New(code.NodeExecFailed, fmt.Sprintf("指令下发失败: %v", err), err))
		return
	}

	// 5. 记录日志并响应
	h.logMgr.RecordLog(utils.GetClientIP(r), "exec_cmd", "node", trigger.TargetIP, trigger.Command, "sent")

	// 返回异步成功标识
	// 前端需要适配：不再直接显示 output，而是提示"指令已发送"
	result := map[string]string{
		"output": fmt.Sprintf("Command '%s' sent via WebSocket.\n(Output handling not yet implemented in async mode)", trigger.Command),
		"status": "async_sent",
	}
	response.Success(w, result)
}

// HandleNodeTerminal 代理节点终端 (反向隧道版)
func (h *ServerHandler) HandleNodeTerminal(w http.ResponseWriter, r *http.Request) {
	nodeIP := r.URL.Query().Get("ip") // 前端传的是 IP，我们需要查 ID

	// 通过 IP 反查 NodeID (因为 Gateway 只能通过 ID 寻址)
	// 这里假设 nodeMgr 提供了一个通过 IP 查 ID 的方法，或者前端直接传 ID
	// 简便起见，遍历查找 (生产环境建议优化)
	var nodeID string
	nodes := h.nodeMgr.GetAllNodes()
	for _, n := range nodes {
		if n.IP == nodeIP {
			nodeID = n.ID
			break
		}
	}
	if nodeID == "" {
		http.Error(w, "Node not found", 404)
		return
	}

	if !h.gateway.IsConnected(nodeID) {
		http.Error(w, "Node offline", 404)
		return
	}

	sessionID := uuid.NewString()

	// 请求 Worker 建立终端隧道
	err := h.gateway.RequestTunnel(nodeID, protocol.TunnelStartRequest{
		SessionID: sessionID,
		Type:      "terminal",
	})
	if err != nil {
		http.Error(w, "Failed to request terminal: "+err.Error(), 500)
		return
	}

	// 等待连接
	workerConn, err := h.gateway.AwaitTunnelConnection(sessionID, 10*time.Second)
	if err != nil {
		http.Error(w, err.Error(), 504)
		return
	}
	defer workerConn.Close()

	// 升级前端
	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer clientConn.Close()

	// 双向管道
	errChan := make(chan error, 2)
	go func() {
		// Worker -> Frontend
		for {
			mt, msg, err := workerConn.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}
			clientConn.WriteMessage(mt, msg)
		}
	}()
	go func() {
		// Frontend -> Worker
		for {
			mt, msg, err := clientConn.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}
			workerConn.WriteMessage(mt, msg)
		}
	}()

	<-errChan
}
