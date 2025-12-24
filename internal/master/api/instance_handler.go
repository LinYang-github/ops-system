package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"ops-system/pkg/code"
	"ops-system/pkg/e"
	"ops-system/pkg/protocol"
	"ops-system/pkg/response"
	"ops-system/pkg/utils"
)

// ==========================================
// 私有辅助方法
// ==========================================

// sendInstanceCommand 向 Worker 发送实例控制指令
func (h *ServerHandler) sendInstanceCommand(inst *protocol.InstanceInfo, action string) error {
	// 1. 获取节点信息 (使用注入的 nodeMgr)
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

	// 3. 发送 HTTP 请求
	targetURL := fmt.Sprintf("http://%s:%d/api/instance/action", node.IP, node.Port)
	return utils.PostJSON(targetURL, reqBytes)
}

// ==========================================
// Handlers
// ==========================================

// DeployInstance 部署实例
// POST /api/deploy
func (h *ServerHandler) DeployInstance(w http.ResponseWriter, r *http.Request) {
	type DeployReq struct {
		SystemID       string `json:"system_id"`
		NodeIP         string `json:"node_ip"`
		ServiceName    string `json:"service_name"`
		ServiceVersion string `json:"service_version"`
	}
	var req DeployReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	// 1. 检查节点
	node, exists := h.nodeMgr.GetNode(req.NodeIP)
	if !exists {
		response.Error(w, e.New(code.NodeOffline, "目标节点不在线", nil))
		return
	}

	// 2. 获取下载链接 (使用 pkgMgr)
	downloadURL, err := h.pkgMgr.GetDownloadURL(req.ServiceName, req.ServiceVersion, r.Host)
	if err != nil {
		response.Error(w, e.New(code.PackageNotFound, "生成下载链接失败", err))
		return
	}

	instanceID := fmt.Sprintf("inst-%d", time.Now().UnixNano())

	// 3. 预先入库 (状态为 deploying)
	h.instMgr.RegisterInstance(&protocol.InstanceInfo{
		ID:             instanceID,
		SystemID:       req.SystemID,
		NodeIP:         req.NodeIP,
		ServiceName:    req.ServiceName,
		ServiceVersion: req.ServiceVersion,
		Status:         "deploying",
	})

	// 触发广播
	h.broadcastUpdate()

	// 4. 构造 Worker 请求
	workerReq := protocol.DeployRequest{
		InstanceID:  instanceID,
		SystemName:  req.SystemID,
		ServiceName: req.ServiceName,
		Version:     req.ServiceVersion,
		DownloadURL: downloadURL,
	}
	reqBody, _ := json.Marshal(workerReq)
	targetURL := fmt.Sprintf("http://%s:%d/api/deploy", node.IP, node.Port)

	// 5. 发送请求 (Worker 异步处理)
	if err := utils.PostJSON(targetURL, reqBody); err != nil {
		// 失败回滚状态
		h.instMgr.UpdateInstanceStatus(instanceID, "error", 0)

		h.logMgr.RecordLog(utils.GetClientIP(r), "deploy_instance", "instance", req.ServiceName, "Failed: "+err.Error(), "fail")
		h.broadcastUpdate()

		response.Error(w, e.New(code.DeployFailed, fmt.Sprintf("Worker 部署请求失败: %v", err), err))
		return
	}

	// 记录日志
	logDetail := fmt.Sprintf("Node: %s, Ver: %s, ID: %s", req.NodeIP, req.ServiceVersion, instanceID)
	h.logMgr.RecordLog(utils.GetClientIP(r), "deploy_instance", "instance", req.ServiceName, logDetail, "success")

	response.Success(w, nil)
}

// RegisterExternal 纳管外部服务
// POST /api/deploy/external
func (h *ServerHandler) RegisterExternal(w http.ResponseWriter, r *http.Request) {
	type RegExtReq struct {
		SystemID string                  `json:"system_id"`
		NodeIP   string                  `json:"node_ip"`
		Config   protocol.ExternalConfig `json:"config"`
	}
	var req RegExtReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	node, exists := h.nodeMgr.GetNode(req.NodeIP)
	if !exists {
		response.Error(w, e.New(code.NodeOffline, "目标节点不在线", nil))
		return
	}

	instanceID := fmt.Sprintf("ext-%d", time.Now().UnixNano())

	// 入库
	h.instMgr.RegisterInstance(&protocol.InstanceInfo{
		ID:             instanceID,
		SystemID:       req.SystemID,
		NodeIP:         req.NodeIP,
		ServiceName:    req.Config.Name,
		ServiceVersion: "external",
		Status:         "stopped",
	})
	h.broadcastUpdate()

	// 发送给 Worker
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
		response.Error(w, e.New(code.DeployFailed, fmt.Sprintf("Worker 纳管请求失败: %v", err), err))
		return
	}

	h.logMgr.RecordLog(utils.GetClientIP(r), "adopt_instance", "instance", req.Config.Name, "External Register", "success")
	response.Success(w, nil)
}

// InstanceAction 单实例启停/销毁
// POST /api/instance/action
func (h *ServerHandler) InstanceAction(w http.ResponseWriter, r *http.Request) {
	var req protocol.InstanceActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	// 查找实例
	inst, ok := h.instMgr.GetInstance(req.InstanceID)
	if !ok {
		response.Error(w, e.New(code.InstanceNotFound, "实例不存在", nil))
		return
	}

	// 发送指令
	if err := h.sendInstanceCommand(inst, req.Action); err != nil {
		h.logMgr.RecordLog(utils.GetClientIP(r), req.Action+"_instance", "instance", inst.ServiceName, "Failed: "+err.Error(), "fail")
		response.Error(w, e.New(code.ActionFailed, fmt.Sprintf("发送指令失败: %v", err), err))
		return
	}

	// 仅销毁操作直接从 DB 删除
	if req.Action == "destroy" {
		h.instMgr.RemoveInstance(req.InstanceID)
		h.broadcastUpdate()
	}

	h.logMgr.RecordLog(utils.GetClientIP(r), req.Action+"_instance", "instance", inst.ServiceName, "ID: "+inst.ID, "success")
	response.Success(w, nil)
}

// WorkerStatusReport Worker 状态上报回调
// POST /api/instance/status_report
func (h *ServerHandler) WorkerStatusReport(w http.ResponseWriter, r *http.Request) {
	var report protocol.InstanceStatusReport
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	// 更新状态
	h.instMgr.UpdateInstanceFullStatus(&report)

	// 触发广播
	h.broadcastUpdate()

	response.Success(w, nil)
}

// SystemAction 系统级批量启停
// POST /api/systems/action
func (h *ServerHandler) SystemAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SystemID string `json:"system_id"`
		Action   string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	// 获取系统下所有实例
	instances, err := h.instMgr.GetSystemInstances(req.SystemID)
	if err != nil {
		response.Error(w, e.New(code.DatabaseError, "查询实例失败", err))
		return
	}

	// 筛选目标
	var targets []*protocol.InstanceInfo
	for i := range instances {
		inst := &instances[i]
		if req.Action == "start" && inst.Status != "running" {
			targets = append(targets, inst)
		} else if req.Action == "stop" && inst.Status == "running" {
			targets = append(targets, inst)
		}
	}

	if len(targets) == 0 {
		response.Success(w, map[string]string{"msg": "没有需要操作的实例"})
		return
	}

	// 并发下发
	var wg sync.WaitGroup
	errCount := 0
	var mu sync.Mutex

	for _, inst := range targets {
		wg.Add(1)
		go func(target *protocol.InstanceInfo) {
			defer wg.Done()
			if err := h.sendInstanceCommand(target, req.Action); err != nil {
				// 仅记录日志，不中断其他
				mu.Lock()
				errCount++
				mu.Unlock()
			}
		}(inst)
	}

	wg.Wait()

	logDetail := fmt.Sprintf("Action: %s, Count: %d, Failed: %d", req.Action, len(targets), errCount)
	h.logMgr.RecordLog(utils.GetClientIP(r), "batch_"+req.Action, "system", req.SystemID, logDetail, "success")

	response.Success(w, map[string]string{
		"msg": fmt.Sprintf("操作完成: %d 成功, %d 失败", len(targets)-errCount, errCount),
	})
}
