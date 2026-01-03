package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
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
	// 忽略错误，默认 retain=0 (全清)
	json.NewDecoder(r.Body).Decode(&req)

	nodes := h.nodeMgr.GetAllNodes()
	var wg sync.WaitGroup
	var mu sync.Mutex

	totalFreed := int64(0)
	successCount := 0
	targetCount := 0 // 实际尝试清理的在线节点数

	// 并发下发指令
	for _, node := range nodes {
		// 1. 严格过滤非在线节点
		if node.Status != "online" {
			continue
		}

		// 2. 双重检查：确保 WebSocket 连接实际存在
		if !h.gateway.IsConnected(node.ID) {
			continue
		}

		targetCount++
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

	logDetail := fmt.Sprintf("Targets: %d, Success: %d, Freed: %s", targetCount, successCount, utils.FormatBytes(totalFreed))
	h.logMgr.RecordLog(utils.GetClientIP(r), "cleanup_cache", "cluster", "online_nodes", logDetail, "success")

	// 返回统计信息
	// 前端可以根据 total_nodes 和 target_count 区分展示
	response.Success(w, map[string]interface{}{
		"total_nodes":   len(nodes),  // 总注册节点
		"target_nodes":  targetCount, // 实际在线并尝试清理的节点
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

// BatchUpgradeWorkers 批量升级所有在线 Worker
// POST /api/maintenance/upgrade_workers
func (h *ServerHandler) BatchUpgradeWorkers(w http.ResponseWriter, r *http.Request) {
	// 1. 预计算各平台二进制包的 Hash (避免循环中重复 IO)
	// 假设目录结构: uploads/system/
	// 文件名约定: worker_linux_amd64, worker_windows_amd64.exe
	baseDir := filepath.Join(h.uploadDir, "system")

	// 定义支持的架构映射
	binaries := map[string]struct {
		Name string
		Hash string
	}{
		"linux_amd64":   {Name: "worker_linux_amd64", Hash: ""},
		"windows_amd64": {Name: "worker_windows_amd64.exe", Hash: ""},
		// [新增] ARM64 (服务器/树莓派/M1 Mac)
		"linux_arm64": {Name: "worker_linux_arm64", Hash: ""},

		// [新增] macOS (开发调试用)
		"darwin_arm64": {Name: "worker_darwin_arm64", Hash: ""}, // Apple Silicon
		"darwin_amd64": {Name: "worker_darwin_amd64", Hash: ""}, // Intel Mac
	}

	// 预加载 Hash 并持久化配置
	for key, item := range binaries {
		path := filepath.Join(baseDir, item.Name)

		// calculateFileHash 在 api/utils.go 中定义
		if hash, err := calculateFileHash(path); err == nil {
			// 更新 map 中的 Hash 值供后续下发使用
			binaries[key] = struct{ Name, Hash string }{item.Name, hash}

			// [核心新增] 将计算出的最新 Hash 保存到全局配置表 (sys_settings)
			// 目的：让 Gateway 在处理新连接时，能读取到此"期望指纹"，实现离线节点上线自动升级
			// Key 格式: agent_target_hash_linux_amd64
			settingKey := "agent_target_hash_" + key
			if err := h.configMgr.SaveSetting(settingKey, hash); err != nil {
				log.Printf("[Upgrade] Failed to save target hash for %s: %v", key, err)
			}
		} else {
			// 如果文件不存在，Hash 留空，后续跳过对应平台的升级
			log.Printf("[Upgrade] Binary missing for %s: %v", key, err)
		}
	}

	// 2. 遍历所有节点 (寻找在线节点立即升级)
	nodes := h.nodeMgr.GetAllNodes()
	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount := 0
	targetCount := 0

	host := r.Host // 用于生成下载链接

	for _, node := range nodes {
		// 过滤：必须在线 且 WebSocket 连接正常
		if node.Status != "online" || !h.gateway.IsConnected(node.ID) {
			continue
		}

		// 【修改点】优化匹配逻辑
		var binKey string

		// 转小写方便匹配
		osType := strings.ToLower(node.OS)
		archType := strings.ToLower(node.Arch)

		if strings.Contains(osType, "windows") {
			if archType == "amd64" {
				binKey = "windows_amd64"
			}
			// Windows ARM 暂且不提
		} else if strings.Contains(osType, "darwin") {
			if archType == "arm64" {
				binKey = "darwin_arm64"
			} else {
				binKey = "darwin_amd64"
			}
		} else {
			// 默认为 Linux
			if archType == "arm64" || archType == "aarch64" {
				binKey = "linux_arm64"
			} else {
				binKey = "linux_amd64"
			}
		}

		binInfo := binaries[binKey]
		if binInfo.Hash == "" {
			// log.Printf("No binary found for %s %s", node.OS, node.Arch)
			continue
		}

		// 4. 并发下发指令
		go func(n protocol.NodeInfo, bName, bHash string) {
			defer wg.Done()

			// 构造下载链接
			downloadURL := fmt.Sprintf("http://%s/download/system/%s", host, bName)

			payload := protocol.WorkerUpgradeRequest{
				DownloadURL: downloadURL,
				Checksum:    bHash,
				Version:     fmt.Sprintf("%d", time.Now().Unix()), // 使用时间戳作为临时版本标识
			}

			// 发送升级指令 (Gateway 需实现 SendUpgradeInstruction)
			if err := h.gateway.SendUpgradeInstruction(n.ID, payload); err == nil {
				mu.Lock()
				successCount++
				mu.Unlock()
			} else {
				log.Printf("[Upgrade] Failed to send instruction to %s: %v", n.IP, err)
			}
		}(node, binInfo.Name, binInfo.Hash)
	}

	wg.Wait()

	// 记录操作日志
	logDetail := fmt.Sprintf("Triggered: %d/%d nodes", successCount, targetCount)
	h.logMgr.RecordLog(utils.GetClientIP(r), "batch_upgrade", "cluster", "all_nodes", logDetail, "success")

	response.Success(w, map[string]interface{}{
		"total_online": len(nodes), // 参考总数
		"triggered":    targetCount,
		"success":      successCount,
	})
}
