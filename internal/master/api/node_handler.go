package api

import (
	"bytes"
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
)

// HandleHeartbeat 处理 Worker 心跳
// POST /api/worker/heartbeat
func (h *ServerHandler) HandleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, e.New(code.MethodNotAllowed, "Method not allowed", nil))
		return
	}

	var req protocol.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// 心跳频繁，JSON错误通常意味着协议不匹配
		response.Error(w, e.New(code.InvalidJSON, "Invalid heartbeat payload", err))
		return
	}

	// 1. 获取连接层 IP
	remoteIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(remoteIP); err == nil {
		remoteIP = host
	}

	// 2. 智能 IP 修正 (处理本地开发环境)
	if (remoteIP == "127.0.0.1" || remoteIP == "::1") &&
		req.Info.IP != "" && req.Info.IP != "127.0.0.1" {
		remoteIP = req.Info.IP
	}

	// 3. 更新数据库 (无锁/低频锁)
	h.nodeMgr.HandleHeartbeat(req, remoteIP)

	// 4. 触发 WebSocket 广播 (Hub 会自动节流)
	ws.BroadcastNodes(h.nodeMgr.GetAllNodes())

	// 心跳接口通常只返回简单文本或标准成功响应
	// 这里为了统一格式返回 standard response，Worker 端通常只检查 http status 200
	response.Success(w, "pong")
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
		IP   string `json:"ip"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	if err := h.nodeMgr.RenameNode(req.IP, req.Name); err != nil {
		response.Error(w, e.New(code.DatabaseError, "重命名失败", err))
		return
	}

	h.logMgr.RecordLog(utils.GetClientIP(r), "rename_node", "node", req.IP, req.Name, "success")
	ws.BroadcastNodes(h.nodeMgr.GetAllNodes())

	response.Success(w, nil)
}

// ResetNodeName 重置节点名为 Hostname
// POST /api/nodes/reset_name
func (h *ServerHandler) ResetNodeName(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	if err := h.nodeMgr.ResetNodeName(req.IP); err != nil {
		response.Error(w, e.New(code.DatabaseError, "重置名称失败", err))
		return
	}

	h.logMgr.RecordLog(utils.GetClientIP(r), "reset_node_name", "node", req.IP, "", "success")
	ws.BroadcastNodes(h.nodeMgr.GetAllNodes())

	response.Success(w, nil)
}

// TriggerCmd 下发 CMD 指令
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

	node, exists := h.nodeMgr.GetNode(trigger.TargetIP)
	if !exists {
		response.Error(w, e.New(code.NodeNotFound, "节点不存在或离线", nil))
		return
	}

	// 构造请求
	workerReq := protocol.CommandRequest{Command: trigger.Command}
	reqBody, _ := json.Marshal(workerReq)

	// 拼接 URL: http://IP:Port/api/exec
	targetURL := fmt.Sprintf("http://%s:%d/api/exec", node.IP, node.Port)

	// 使用 HTTP Client 请求 Worker
	client := &http.Client{Timeout: 10 * time.Second} // 执行命令可能稍慢
	resp, err := client.Post(targetURL, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		h.logMgr.RecordLog(utils.GetClientIP(r), "exec_cmd", "node", trigger.TargetIP, "Network Error", "fail")
		response.Error(w, e.New(code.NodeExecFailed, fmt.Sprintf("连接Worker失败: %v", err), err))
		return
	}
	defer resp.Body.Close()

	// 解析 Worker 响应
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		response.Error(w, e.New(code.NodeExecFailed, "解析Worker响应失败", err))
		return
	}

	// 记录日志
	status := "success"
	if result["error"] != "" {
		status = "fail"
	}
	h.logMgr.RecordLog(utils.GetClientIP(r), "exec_cmd", "node", trigger.TargetIP, trigger.Command, status)

	// 返回结果
	response.Success(w, result)
}
