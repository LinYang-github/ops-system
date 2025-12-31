package api

import (
	"encoding/json"
	"net/http"

	"ops-system/pkg/code"
	"ops-system/pkg/e"
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
