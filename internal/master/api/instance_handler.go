package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"ops-system/pkg/protocol"
	"ops-system/pkg/utils"
)

// ==========================================
// 公共辅助函数
// ==========================================

// sendInstanceCommand 向 Worker 发送实例控制指令
func (h *ServerHandler) sendInstanceCommand(inst *protocol.InstanceInfo, action string) error {
	// 1. 获取节点信息
	node, exists := h.nodeMgr.GetNode(inst.NodeIP)
	if !exists {
		return fmt.Errorf("node %s offline", inst.NodeIP)
	}

	// 2. 构造请求
	workerReq := protocol.InstanceActionRequest{
		InstanceID: inst.ID,
		Action:     action,
	}
	reqBytes, _ := json.Marshal(workerReq)

	// 3. 发送 HTTP 请求 (使用 utils.PostJSON 复用连接)
	targetURL := fmt.Sprintf("http://%s:%d/api/instance/action", node.IP, node.Port)
	return utils.PostJSON(targetURL, reqBytes)
}

// ==========================================
// Handlers
// ==========================================

// handleDeployInstance 部署实例
func (h *ServerHandler) DeployInstance(w http.ResponseWriter, r *http.Request) {
	type DeployReq struct {
		SystemID       string `json:"system_id"`
		NodeIP         string `json:"node_ip"`
		ServiceName    string `json:"service_name"`
		ServiceVersion string `json:"service_version"`
	}
	var req DeployReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	node, exists := h.nodeMgr.GetNode(req.NodeIP)
	if !exists {
		http.Error(w, "Node offline", 404)
		return
	}

	downloadURL, err := h.pkgMgr.GetDownloadURL(req.ServiceName, req.ServiceVersion, r.Host)
	if err != nil {
		http.Error(w, "Generate URL failed: "+err.Error(), 500)
		return
	}
	instanceID := fmt.Sprintf("inst-%d", time.Now().UnixNano())

	// 1. 使用 instManager 注册
	h.instMgr.RegisterInstance(&protocol.InstanceInfo{
		ID:             instanceID,
		SystemID:       req.SystemID,
		NodeIP:         req.NodeIP,
		ServiceName:    req.ServiceName,
		ServiceVersion: req.ServiceVersion,
		Status:         "deploying",
	})

	h.broadcastUpdate()

	// 2. 构造 Worker 请求
	workerReq := protocol.DeployRequest{
		InstanceID:  instanceID,
		SystemName:  req.SystemID,
		ServiceName: req.ServiceName,
		Version:     req.ServiceVersion,
		DownloadURL: downloadURL,
	}
	reqBody, _ := json.Marshal(workerReq)
	targetURL := fmt.Sprintf("http://%s:%d/api/deploy", node.IP, node.Port)

	// 3. 发送请求
	if err := utils.PostJSON(targetURL, reqBody); err != nil {
		h.instMgr.UpdateInstanceStatus(instanceID, "error", 0)
		h.logMgr.RecordLog(utils.GetClientIP(r), "deploy_instance", "instance", req.ServiceName, "Failed: "+err.Error(), "fail")
		h.broadcastUpdate()
		http.Error(w, fmt.Sprintf("Worker deploy failed: %v", err), 500)
		return
	}

	logDetail := fmt.Sprintf("Node: %s, Ver: %s, ID: %s", req.NodeIP, req.ServiceVersion, instanceID)
	h.logMgr.RecordLog(utils.GetClientIP(r), "deploy_instance", "instance", req.ServiceName, logDetail, "success")

	w.Write([]byte(`{"status":"ok"}`))
}

// handleInstanceAction 单实例启停
func (h *ServerHandler) InstanceAction(w http.ResponseWriter, r *http.Request) {
	var req protocol.InstanceActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	// 使用 instManager 获取实例
	inst, ok := h.instMgr.GetInstance(req.InstanceID)
	if !ok {
		http.Error(w, "Instance not found", 404)
		return
	}

	// 发送指令
	if err := h.sendInstanceCommand(inst, req.Action); err != nil {
		h.logMgr.RecordLog(utils.GetClientIP(r), req.Action+"_instance", "instance", inst.ServiceName, "Failed: "+err.Error(), "fail")
		http.Error(w, fmt.Sprintf("Failed to send command: %v", err), 500)
		return
	}

	if req.Action == "destroy" {
		h.instMgr.RemoveInstance(req.InstanceID)
		h.broadcastUpdate()
	}

	h.logMgr.RecordLog(utils.GetClientIP(r), req.Action+"_instance", "instance", inst.ServiceName, "ID: "+inst.ID, "success")
	w.Write([]byte(`{"status":"ok"}`))
}

// handleWorkerInstanceStatusReport 状态上报
func (h *ServerHandler) WorkerInstanceStatusReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	var report protocol.InstanceStatusReport
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		http.Error(w, "Invalid JSON", 400)
		return
	}

	// 使用 instManager 更新
	h.instMgr.UpdateInstanceFullStatus(&report)
	h.broadcastUpdate()

	w.Write([]byte(`{"status":"ok"}`))
}

// handleSystemAction 批量操作
func (h *ServerHandler) SystemAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}

	var req struct {
		SystemID string `json:"system_id"`
		Action   string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", 400)
		return
	}

	// 使用 instManager
	instances, err := h.instMgr.GetSystemInstances(req.SystemID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	var targets []*protocol.InstanceInfo
	for i := range instances {
		inst := &instances[i]
		// 修复了这里的 if-else 语法错误
		if req.Action == "start" && inst.Status != "running" {
			targets = append(targets, inst)
		} else if req.Action == "stop" && inst.Status == "running" {
			targets = append(targets, inst)
		}
	}

	if len(targets) == 0 {
		w.Write([]byte(`{"status":"ok", "msg":"No instances need action"}`))
		return
	}

	var wg sync.WaitGroup
	errCount := 0
	var mu sync.Mutex

	for _, inst := range targets {
		wg.Add(1)
		go func(target *protocol.InstanceInfo) {
			defer wg.Done()
			if err := h.sendInstanceCommand(target, req.Action); err != nil {
				log.Printf("[BatchError] ID=%s Action=%s Err=%v", target.ID, req.Action, err)
				mu.Lock()
				errCount++
				mu.Unlock()
			}
		}(inst)
	}

	wg.Wait()

	logDetail := fmt.Sprintf("Action: %s, Count: %d, Failed: %d", req.Action, len(targets), errCount)
	h.logMgr.RecordLog(utils.GetClientIP(r), "batch_"+req.Action, "system", req.SystemID, logDetail, "success")

	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"msg":    fmt.Sprintf("Done %d, Fail %d", len(targets), errCount),
	})
}

// handleRegisterExternal 注册纳管实例
func (h *ServerHandler) RegisterExternal(w http.ResponseWriter, r *http.Request) {
	// 1. 解析请求 (包含 Config 和 NodeIP)
	type RegExtReq struct {
		SystemID string                  `json:"system_id"`
		NodeIP   string                  `json:"node_ip"`
		Config   protocol.ExternalConfig `json:"config"`
	}
	var req RegExtReq
	json.NewDecoder(r.Body).Decode(&req)

	node, exists := h.nodeMgr.GetNode(req.NodeIP)
	if !exists {
		http.Error(w, "Node offline", 404)
		return
	}

	instanceID := fmt.Sprintf("ext-%d", time.Now().UnixNano())

	// 2. 入库 (服务名直接用 Config.Name)
	h.instMgr.RegisterInstance(&protocol.InstanceInfo{
		ID: instanceID, SystemID: req.SystemID, NodeIP: req.NodeIP,
		ServiceName: req.Config.Name, ServiceVersion: "external",
		Status: "stopped",
	})
	h.broadcastUpdate()

	// 3. 发送给 Worker
	workerReq := protocol.RegisterExternalRequest{
		InstanceID: instanceID,
		SystemName: req.SystemID,
		Config:     req.Config,
	}
	reqBytes, _ := json.Marshal(workerReq)
	targetURL := fmt.Sprintf("http://%s:%d/api/external/register", node.IP, node.Port)

	if err := utils.PostJSON(targetURL, reqBytes); err != nil {
		h.instMgr.UpdateInstanceStatus(instanceID, "error", 0)
		h.broadcastUpdate()
		http.Error(w, fmt.Sprintf("Worker register failed: %v", err), 500)
		return
	}

	w.Write([]byte(`{"status":"ok"}`))
}
