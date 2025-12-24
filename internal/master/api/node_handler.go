package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"ops-system/internal/master/ws"
	"ops-system/pkg/protocol"
	"ops-system/pkg/utils"
)

// handleHeartbeat 处理心跳
func (h *ServerHandler) HandleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}

	var req protocol.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", 400)
		return
	}

	// 1. 获取连接层的 IP
	remoteIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(remoteIP); err == nil {
		remoteIP = host
	}

	// 【修改点】：智能 IP 修正
	// 如果连接来自本机 (127.0.0.1 或 ::1)，且 Worker 自己汇报了一个非回环 IP
	// 那么我们优先使用 Worker 汇报的 IP (例如 192.168.x.x)
	// 这样在本地测试时，界面就能显示局域网 IP 而不是 localhost
	if (remoteIP == "127.0.0.1" || remoteIP == "::1") &&
		req.Info.IP != "" && req.Info.IP != "127.0.0.1" {
		remoteIP = req.Info.IP
	}

	// 2. 更新数据库
	h.nodeMgr.HandleHeartbeat(req, remoteIP)

	// 3. 广播
	ws.BroadcastNodes(h.nodeMgr.GetAllNodes())

	w.Write([]byte("pong"))
}

// handleListNodes 获取列表
func (h *ServerHandler) ListNodes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.nodeMgr.GetAllNodes())
}

// handleAddNode 添加规划节点
func (h *ServerHandler) AddNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP   string `json:"ip"`
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if err := h.nodeMgr.AddPlannedNode(req.IP, req.Name); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	ws.BroadcastNodes(h.nodeMgr.GetAllNodes())
	w.Write([]byte(`{"status":"ok"}`))
}

// handleDeleteNode 删除节点
func (h *ServerHandler) DeleteNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP string `json:"ip"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if err := h.nodeMgr.DeleteNode(req.IP); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// 记录日志
	h.logMgr.RecordLog(utils.GetClientIP(r), "delete_node", "node", req.IP, "", "success")
	ws.BroadcastNodes(h.nodeMgr.GetAllNodes())
	w.Write([]byte(`{"status":"ok"}`))
}

// handleRenameNode 重命名
func (h *ServerHandler) RenameNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP   string `json:"ip"`
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if err := h.nodeMgr.RenameNode(req.IP, req.Name); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	ws.BroadcastNodes(h.nodeMgr.GetAllNodes())
	w.Write([]byte(`{"status":"ok"}`))
}

// handleResetNodeName 重置名称
func (h *ServerHandler) ResetNodeName(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP string `json:"ip"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if err := h.nodeMgr.ResetNodeName(req.IP); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	ws.BroadcastNodes(h.nodeMgr.GetAllNodes())
	w.Write([]byte(`{"status":"ok"}`))
}

// handleTriggerCmd 下发 CMD 指令 (调试用)
func (h *ServerHandler) TriggerCmd(w http.ResponseWriter, r *http.Request) {
	type TriggerReq struct {
		TargetIP string `json:"target_ip"`
		Command  string `json:"command"`
	}

	var trigger TriggerReq
	if err := json.NewDecoder(r.Body).Decode(&trigger); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	node, exists := h.nodeMgr.GetNode(trigger.TargetIP)
	if !exists {
		http.Error(w, "Node not found or offline", 404)
		return
	}

	workerReq := protocol.CommandRequest{Command: trigger.Command}
	reqBody, _ := json.Marshal(workerReq)

	targetURL := fmt.Sprintf("http://%s:%d/api/exec", node.IP, node.Port)

	// 这里直接透传 Worker 的响应，或者使用 utils.PostJSON 但由于需要返回 output 内容，
	// 简单的 PostJSON 可能不够，这里简单用 http.Client
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(targetURL, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed: %v", err), 502)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	// 记录日志
	h.logMgr.RecordLog(utils.GetClientIP(r), "exec_cmd", "node", trigger.TargetIP, trigger.Command, "success")

	// 透传 Body
	// io.Copy(w, resp.Body) // 需要 import io
	// 或者简单点：
	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	json.NewEncoder(w).Encode(result)
}
