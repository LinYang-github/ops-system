package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
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
		response.Error(w, e.New(code.InvalidJSON, "Invalid heartbeat payload", err))
		return
	}

	remoteIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(remoteIP); err == nil {
		remoteIP = host
	}
	if (remoteIP == "127.0.0.1" || remoteIP == "::1") && req.Info.IP != "" && req.Info.IP != "127.0.0.1" {
		remoteIP = req.Info.IP
	}

	// 1. 更新数据库 (打印日志以便调试)
	// 如果 nodeManager 内部有错误，它会打印到控制台
	h.nodeMgr.HandleHeartbeat(req, remoteIP)

	// 2. 广播 (确保 ws 包导入正确)
	ws.BroadcastNodes(h.nodeMgr.GetAllNodes())

	// 3. 获取动态配置
	var hbInterval, monInterval int64 = 5, 3
	if globalCfg, err := h.configMgr.GetGlobalConfig(); err == nil {
		hbInterval = int64(globalCfg.Worker.HeartbeatInterval)
		monInterval = int64(globalCfg.Worker.MonitorInterval)
	}

	// 4. 返回标准响应
	resp := protocol.HeartbeatResponse{
		Code:              200, // 这里的 Code 仅仅是 payload 里的一个字段，不是外层信封的 Code
		HeartbeatInterval: hbInterval,
		MonitorInterval:   monInterval,
	}

	// 最终返回 JSON: { "code": 0, "msg": "success", "data": { "code": 200, "heartbeat_interval": 5... } }
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

// CleanNodeCache 远程清理节点缓存
// POST /api/nodes/clean_cache
func (h *ServerHandler) CleanNodeCache(w http.ResponseWriter, r *http.Request) {
	type CleanReq struct {
		NodeIP string `json:"node_ip"`
		Retain int    `json:"retain"`
	}
	var req CleanReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON error", err))
		return
	}

	node, exists := h.nodeMgr.GetNode(req.NodeIP)
	if !exists {
		response.Error(w, e.New(code.NodeNotFound, "Node offline", nil))
		return
	}

	// 转发请求给 Worker
	targetURL := fmt.Sprintf("http://%s:%d/api/maintenance/cleanup_cache", node.IP, node.Port)
	workerPayload := map[string]int{"retain": req.Retain}
	payloadBytes, _ := json.Marshal(workerPayload)

	respBody, err := utils.Post(targetURL, payloadBytes)
	if err != nil {
		response.Error(w, e.New(code.NetworkError, "Call worker failed", err))
		return
	}

	// 透传 Worker 的响应
	w.Header().Set("Content-Type", "application/json")
	w.Write(respBody)
}

// CleanAllNodesCache 清理所有在线节点的缓存
// POST /api/maintenance/cleanup_all_cache
func (h *ServerHandler) CleanAllNodesCache(w http.ResponseWriter, r *http.Request) {
	// 1. 解析请求参数 (保留几个版本)
	var req struct {
		Retain int `json:"retain"`
	}
	// 允许不传 body，默认 retain=0 (全删)
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.Error(w, e.New(code.InvalidJSON, "JSON error", err))
			return
		}
	}

	// 2. 获取所有在线节点
	allNodes := h.nodeMgr.GetAllNodes()
	var targetNodes []protocol.NodeInfo
	for _, n := range allNodes {
		if n.Status == "online" {
			targetNodes = append(targetNodes, n)
		}
	}

	if len(targetNodes) == 0 {
		response.Success(w, map[string]interface{}{
			"success_count": 0,
			"total_freed":   0,
			"msg":           "当前无在线节点",
		})
		return
	}

	// 3. 并发调用 Worker
	var wg sync.WaitGroup
	var successCount int64
	var failCount int64
	var totalFreedBytes int64

	// 构造发送给 Worker 的 payload
	workerPayload := map[string]int{"retain": req.Retain}
	payloadBytes, _ := json.Marshal(workerPayload)

	// 限制并发数为 50，防止瞬间耗尽 Master 文件句柄或带宽
	sem := make(chan struct{}, 50)

	for _, node := range targetNodes {
		wg.Add(1)
		sem <- struct{}{} // 获取令牌

		go func(n protocol.NodeInfo) {
			defer wg.Done()
			defer func() { <-sem }() // 释放令牌

			targetURL := fmt.Sprintf("http://%s:%d/api/maintenance/cleanup_cache", n.IP, n.Port)

			// 调用 Worker (复用 utils.Post 自动带 Token)
			respBody, err := utils.Post(targetURL, payloadBytes)
			if err != nil {
				atomic.AddInt64(&failCount, 1)
				return
			}

			// 解析 Worker 响应计算释放空间
			// Worker 返回结构: { code:0, data: { freed_bytes: 1024 ... } }
			var workerResp struct {
				Code int `json:"code"`
				Data struct {
					FreedBytes int64 `json:"freed_bytes"`
				} `json:"data"`
			}

			if json.Unmarshal(respBody, &workerResp) == nil && workerResp.Code == 0 {
				atomic.AddInt64(&successCount, 1)
				atomic.AddInt64(&totalFreedBytes, workerResp.Data.FreedBytes)
			} else {
				atomic.AddInt64(&failCount, 1)
			}
		}(node)
	}

	wg.Wait()

	// 4. 记录操作日志
	logDetail := fmt.Sprintf("Retain: %d, Success: %d, Fail: %d, Freed: %s",
		req.Retain, successCount, failCount, utils.FormatBytes(totalFreedBytes))
	h.logMgr.RecordLog(utils.GetClientIP(r), "clean_all_cache", "cluster", "all_nodes", logDetail, "success")

	// 5. 返回汇总结果
	response.Success(w, map[string]interface{}{
		"success_count": successCount,
		"fail_count":    failCount,
		"total_nodes":   len(targetNodes),
		"total_freed":   totalFreedBytes,
	})
}
