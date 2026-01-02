package api

import (
	"encoding/json"
	"fmt"
	"log"
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

// sendInstanceCommand 向 Worker 发送实例控制指令 (WebSocket 模式)
func (h *ServerHandler) sendInstanceCommand(inst *protocol.InstanceInfo, action string) error {
	node, exists := h.nodeMgr.GetNode(inst.NodeID) // 这里 inst.NodeID 是 UUID
	if !exists || node.Status != "online" {
		return fmt.Errorf("node %s offline", inst.NodeID)
	}

	workerReq := protocol.InstanceActionRequest{
		InstanceID: inst.ID,
		Action:     action,
	}

	return h.gateway.SendCommand(node.ID, workerReq)
}

// ==========================================
// Handlers
// ==========================================

// DeployInstance 部署实例
// POST /api/deploy
func (h *ServerHandler) DeployInstance(w http.ResponseWriter, r *http.Request) {
	type DeployReq struct {
		SystemID       string `json:"system_id"`
		NodeID         string `json:"node_id"`
		ServiceName    string `json:"service_name"`
		ServiceVersion string `json:"service_version"`
	}
	var req DeployReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	var targetNodeID string

	// ==========================================
	// 1. 调度逻辑 (自动选择节点)
	// ==========================================
	if req.NodeID == "auto" {
		// 获取所有节点数据的快照 (包含实时 CPU/Mem)
		allNodes := h.nodeMgr.GetAllNodes()

		// 调度算法选择
		selectedID, found := h.scheduler.SelectBestNode(allNodes)
		if !found {
			response.Error(w, e.New(code.NodeOffline, "自动调度失败: 无可用在线节点", nil))
			return
		}
		targetNodeID = selectedID
	} else {
		targetNodeID = req.NodeID
	}

	node, exists := h.nodeMgr.GetNode(targetNodeID)
	if !exists || node.Status != "online" {
		response.Error(w, e.New(code.NodeOffline, "节点离线或不存在", nil))
		return
	}

	// ==========================================
	// 2. 检查节点状态
	// ==========================================
	// 确保节点在线且 WebSocket 连接已建立
	if !h.gateway.IsConnected(node.ID) {
		log.Printf("[Deploy] 目标节点未连接 (WebSocket Disconnected): ID=%s, IP=%s", node.ID, node.IP)
		response.Error(w, e.New(code.NodeOffline, "目标节点未连接 (WebSocket Disconnected)", nil))
		return
	}

	// ==========================================
	// 3. 配置融合 (核心逻辑)
	// ==========================================

	// 3.1 获取服务包默认配置
	manifest, err := h.pkgMgr.GetManifest(req.ServiceName, req.ServiceVersion)
	if err != nil {
		response.Error(w, e.New(code.PackageNotFound, "找不到服务包定义", err))
		return
	}

	// 3.2 获取系统模块覆盖配置 (可能为空)
	moduleCfg, _ := h.sysMgr.GetModule(req.SystemID, req.ServiceName, req.ServiceVersion)

	// 3.3 融合逻辑
	finalType := manifest.ReadinessType
	finalTarget := manifest.ReadinessTarget
	finalTimeout := manifest.ReadinessTimeout

	if moduleCfg != nil {
		if moduleCfg.ReadinessType != "" {
			finalType = moduleCfg.ReadinessType
		}
		if moduleCfg.ReadinessTarget != "" {
			finalTarget = moduleCfg.ReadinessTarget
		}
		if moduleCfg.ReadinessTimeout > 0 {
			finalTimeout = moduleCfg.ReadinessTimeout
		}
	}

	// ==========================================
	// 4. 准备资源与入库
	// ==========================================

	// 获取下载链接
	downloadURL, err := h.pkgMgr.GetDownloadURL(req.ServiceName, req.ServiceVersion, r.Host)
	if err != nil {
		response.Error(w, e.New(code.PackageNotFound, "生成下载链接失败", err))
		return
	}

	instanceID := fmt.Sprintf("inst-%d", time.Now().UnixNano())

	// 预先入库 (状态为 deploying)
	h.instMgr.RegisterInstance(&protocol.InstanceInfo{
		ID:             instanceID,
		SystemID:       req.SystemID,
		NodeID:         targetNodeID,
		ServiceName:    req.ServiceName,
		ServiceVersion: req.ServiceVersion,
		Status:         "deploying",
	})

	// 触发广播
	h.broadcastUpdate()

	// ==========================================
	// 5. 下发指令给 Worker (WebSocket)
	// ==========================================

	workerReq := protocol.DeployRequest{
		InstanceID:       instanceID,
		SystemName:       req.SystemID,
		ServiceName:      req.ServiceName,
		Version:          req.ServiceVersion,
		DownloadURL:      downloadURL,
		ReadinessType:    finalType,
		ReadinessTarget:  finalTarget,
		ReadinessTimeout: finalTimeout,
	}

	// 使用 WebSocket 下发
	if err := h.gateway.SendCommand(node.ID, workerReq); err != nil {
		// 失败回滚状态
		h.instMgr.UpdateInstanceStatus(instanceID, "error", 0)

		h.logMgr.RecordLog(utils.GetClientIP(r), "deploy_instance", "instance", req.ServiceName, "Failed: "+err.Error(), "fail")
		h.broadcastUpdate()

		response.Error(w, e.New(code.DeployFailed, fmt.Sprintf("指令下发失败: %v", err), err))
		return
	}

	// 记录成功日志
	logDetail := fmt.Sprintf("Node: %s, Ver: %s, ID: %s", targetNodeID, req.ServiceVersion, instanceID)
	h.logMgr.RecordLog(utils.GetClientIP(r), "deploy_instance", "instance", req.ServiceName, logDetail, "success")

	response.Success(w, nil)
}

// RegisterExternal 纳管外部服务
// POST /api/deploy/external
func (h *ServerHandler) RegisterExternal(w http.ResponseWriter, r *http.Request) {
	type RegExtReq struct {
		SystemID string                  `json:"system_id"`
		NodeID   string                  `json:"node_id"`
		Config   protocol.ExternalConfig `json:"config"`
	}
	var req RegExtReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	// 检查连接
	if !h.gateway.IsConnected(req.NodeID) {
		response.Error(w, e.New(code.NodeOffline, "目标节点未连接", nil))
		return
	}

	instanceID := fmt.Sprintf("ext-%d", time.Now().UnixNano())

	// 入库
	h.instMgr.RegisterInstance(&protocol.InstanceInfo{
		ID:             instanceID,
		SystemID:       req.SystemID,
		NodeID:         req.NodeID,
		ServiceName:    req.Config.Name,
		ServiceVersion: "external",
		Status:         "stopped",
	})
	h.broadcastUpdate()

	// 发送给 Worker (WebSocket)
	workerReq := protocol.RegisterExternalRequest{
		InstanceID: instanceID,
		SystemName: req.SystemID,
		Config:     req.Config,
	}

	if err := h.gateway.SendCommand(req.NodeID, workerReq); err != nil {
		h.instMgr.UpdateInstanceStatus(instanceID, "error", 0)
		h.broadcastUpdate()
		response.Error(w, e.New(code.DeployFailed, fmt.Sprintf("指令下发失败: %v", err), err))
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

	// 仅销毁操作可以直接从 DB 删除（如果节点离线也能删）
	// 但通常最好也通知 Worker 清理
	if req.Action == "destroy" && !h.gateway.IsConnected(inst.NodeID) {
		// 节点不在线，强制删除元数据
		h.instMgr.RemoveInstance(req.InstanceID)
		h.broadcastUpdate()
		h.logMgr.RecordLog(utils.GetClientIP(r), "destroy_instance", "instance", inst.ServiceName, "Force delete (node offline)", "success")
		response.Success(w, nil)
		return
	}

	// 发送指令
	if err := h.sendInstanceCommand(inst, req.Action); err != nil {
		h.logMgr.RecordLog(utils.GetClientIP(r), req.Action+"_instance", "instance", inst.ServiceName, "Failed: "+err.Error(), "fail")
		response.Error(w, e.New(code.ActionFailed, fmt.Sprintf("指令下发失败: %v", err), err))
		return
	}

	// 如果是 destroy，Worker 执行成功后一般不再上报状态，
	// 这里可以先标记，等待 Worker 响应（异步架构下比较复杂），
	// 或者直接在前端刷新时处理。
	// 为了用户体验，我们假定指令发送成功后，稍后会删除。
	// 如果是同步架构，这里会等待。WS 是异步的。
	if req.Action == "destroy" {
		// 乐观策略：稍后 Worker 会处理并可能清理文件
		// Master 端可以先不删，等用户再次确认或通过回调删除
		// 简单起见：这里直接删库，Worker 尽力而为
		h.instMgr.RemoveInstance(req.InstanceID)
		h.broadcastUpdate()
	}

	h.logMgr.RecordLog(utils.GetClientIP(r), req.Action+"_instance", "instance", inst.ServiceName, "ID: "+inst.ID, "success")
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
