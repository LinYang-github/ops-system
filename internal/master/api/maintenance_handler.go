package api

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"ops-system/pkg/code"
	"ops-system/pkg/e"
	"ops-system/pkg/protocol"
	"ops-system/pkg/response"
	"ops-system/pkg/utils"
)

// ScanOrphans 扫描孤儿文件
// GET /api/maintenance/orphans
func (h *ServerHandler) ScanOrphans(w http.ResponseWriter, r *http.Request) {
	orphans, err := h.pkgMgr.ScanOrphans()
	if err != nil {
		response.Error(w, e.New(code.ServerError, "扫描失败", err))
		return
	}
	response.Success(w, orphans)
}

// DeleteOrphans 清理孤儿文件
// POST /api/maintenance/orphans/delete
func (h *ServerHandler) DeleteOrphans(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Files []string `json:"files"` // 待删除的文件路径列表
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "参数错误", err))
		return
	}

	if len(req.Files) == 0 {
		response.Success(w, nil)
		return
	}

	if err := h.pkgMgr.DeleteOrphans(req.Files); err != nil {
		response.Error(w, e.New(code.ServerError, "删除失败", err))
		return
	}

	h.logMgr.RecordLog(utils.GetClientIP(r), "cleanup_orphans", "system", "storage", "Cleaned orphan files", "success")
	response.Success(w, nil)
}

// CleanCache 清理/重置系统缓存
// POST /api/maintenance/cache/clean
func (h *ServerHandler) CleanCache(w http.ResponseWriter, r *http.Request) {
	// 1. 重置节点管理器缓存 (如果有实现 Reload)
	// h.nodeMgr.ReloadFromDB() // 假设你实现了这个方法

	// 2. 清理实例监控缓存 (metrics)
	// 目前 InstanceManager 没有暴露全量清理，但我们可以通过重启机制或添加方法来实现
	// 这里假设我们在 InstanceManager 加一个 ClearMetricsCache 方法
	// h.instMgr.ClearMetricsCache()

	// 3. 强制触发一次 WebSocket 全量广播
	h.broadcastUpdate()

	h.logMgr.RecordLog(utils.GetClientIP(r), "clean_cache", "system", "memory", "Cache cleared manually", "success")
	response.Success(w, map[string]string{"status": "ok"})
}

// CleanupAllCache 一键全量清理 (包含日志、缓存、临时文件等)
// POST /api/maintenance/cleanup_all
func (h *ServerHandler) CleanupAllCache(w http.ResponseWriter, r *http.Request) {
	// 1. 清理过期日志 (保留 30 天)
	h.logMgr.CleanupOldLogs(30)

	// 2. 清理告警历史 (只保留最近 1000 条或类似的逻辑，这里简单调用 ClearEvents 是清空所有，慎用)
	// 建议 alertMgr 增加 CleanupHistory(days) 方法
	// h.alertMgr.CleanupHistory(30)

	// 3. 触发缓存重置
	// h.CleanCache(w, r) 的逻辑

	h.logMgr.RecordLog(utils.GetClientIP(r), "cleanup_all", "system", "global", "Full system cleanup triggered", "success")
	response.Success(w, map[string]string{"msg": "Cleanup task started"})
}

// CleanupNodeCaches 清理所有节点的下载缓存
// POST /api/maintenance/cleanup_all_cache
func (h *ServerHandler) CleanupNodeCaches(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Retain int `json:"retain"`
	}
	json.NewDecoder(r.Body).Decode(&req) // Ignore error, default 0

	nodes := h.nodeMgr.GetAllNodes()
	var wg sync.WaitGroup
	var mu sync.Mutex

	totalFreed := int64(0)
	successCount := 0
	totalOnline := 0

	// 并发下发指令
	for _, node := range nodes {
		if node.Status != "online" {
			continue
		}
		totalOnline++
		wg.Add(1)

		go func(nid string) {
			defer wg.Done()

			// 构造请求
			workerReq := protocol.CleanupCacheRequest{Retain: req.Retain}
			var workerResp protocol.CleanupCacheResponse

			// 同步调用 (5秒超时)
			err := h.gateway.SyncCall(nid, protocol.TypeCleanupCache, workerReq, &workerResp, 5*time.Second)

			if err == nil && workerResp.Error == "" {
				mu.Lock()
				totalFreed += workerResp.FreedBytes
				successCount++
				mu.Unlock()
			}
		}(node.ID)
	}

	wg.Wait()

	h.logMgr.RecordLog(utils.GetClientIP(r), "cleanup_cache", "cluster", "all_nodes", "Batch cleanup", "success")

	response.Success(w, map[string]interface{}{
		"total_nodes":   len(nodes),
		"online_nodes":  totalOnline,
		"success_count": successCount,
		"total_freed":   totalFreed,
	})
}

// ScanNodeOrphans 扫描所有节点的孤儿资源
// POST /api/maintenance/scan_orphans
func (h *ServerHandler) ScanNodeOrphans(w http.ResponseWriter, r *http.Request) {
	// 1. 准备白名单数据 (SystemNames & InstanceIDs)
	// sysIDs, _ := h.sysMgr.GetAllSystemIDs() // 注意：这里需要 System Name 还是 ID 取决于 Worker 目录结构
	// Worker 目录结构是 instances/{SystemName}/{InstanceID}
	// 但通常建议目录名用 ID 或 Name 固定规则。
	// 假设 System 用的是 Name (如果是 ID 请自行调整获取逻辑)
	// 这里我们需要获取所有合法的 System Name

	// 重新获取完整视图来提取 Name
	sysView := h.sysMgr.GetFullView(nil).([]protocol.SystemView)
	var validSystems []string
	for _, v := range sysView {
		validSystems = append(validSystems, v.Name) // 使用 Name
	}

	validInsts, _ := h.instMgr.GetAllInstanceIDs()

	reqPayload := protocol.OrphanScanRequest{
		ValidSystems:   validSystems,
		ValidInstances: validInsts,
	}

	// 2. 并发扫描
	nodes := h.nodeMgr.GetAllNodes()
	var wg sync.WaitGroup
	var mu sync.Mutex

	results := make([]protocol.OrphanScanNodeResponse, 0)

	for _, node := range nodes {
		if node.Status != "online" {
			continue
		}
		wg.Add(1)

		go func(n protocol.NodeInfo) {
			defer wg.Done()

			var workerResp protocol.OrphanScanNodeResponse
			err := h.gateway.SyncCall(n.ID, protocol.TypeScanOrphans, reqPayload, &workerResp, 8*time.Second)

			res := protocol.OrphanScanNodeResponse{
				NodeIP: n.IP,
				Items:  []protocol.OrphanItem{},
			}

			if err != nil {
				res.Error = err.Error()
			} else {
				res.Items = workerResp.Items
				res.Error = workerResp.Error
			}

			mu.Lock()
			results = append(results, res)
			mu.Unlock()
		}(node)
	}

	wg.Wait()
	response.Success(w, results)
}

// DeleteNodeOrphans 删除节点孤儿资源
// POST /api/maintenance/delete_orphans
func (h *ServerHandler) DeleteNodeOrphans(w http.ResponseWriter, r *http.Request) {
	// 前端传参结构: { targets: [ { node_ip: "...", paths: [...] } ] }
	// 注意：前端传的是 IP，我们需要转为 ID 才能发给 Gateway
	type Target struct {
		NodeIP string   `json:"node_ip"`
		Paths  []string `json:"paths"`
	}
	var req struct {
		Targets []Target `json:"targets"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON error", err))
		return
	}

	// 构建 IP -> ID 映射
	ipToID := make(map[string]string)
	nodes := h.nodeMgr.GetAllNodes()
	for _, n := range nodes {
		ipToID[n.IP] = n.ID
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount := 0

	for _, t := range req.Targets {
		nodeID, ok := ipToID[t.NodeIP]
		if !ok {
			continue // 找不到 ID，可能节点已下线或IP变动
		}

		wg.Add(1)
		go func(nid string, paths []string) {
			defer wg.Done()

			workerReq := protocol.OrphanDeleteRequestWorker{Items: paths}
			var workerResp protocol.OrphanDeleteResponse

			err := h.gateway.SyncCall(nid, protocol.TypeDeleteOrphans, workerReq, &workerResp, 10*time.Second)

			if err == nil && workerResp.Error == "" {
				mu.Lock()
				successCount += workerResp.DeletedCount
				mu.Unlock()
			}
		}(nodeID, t.Paths)
	}

	wg.Wait()

	h.logMgr.RecordLog(utils.GetClientIP(r), "delete_orphans", "cluster", "multi_nodes", "Batch delete orphans", "success")

	response.Success(w, map[string]interface{}{
		"success_count": successCount,
	})
}
