package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"ops-system/internal/worker/executor"
	"ops-system/pkg/protocol"

	"github.com/gorilla/websocket" // 引入 websocket 包
)

// [修复] 定义包级 upgrader，供 log_stream.go 和 terminal.go 使用
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// WorkerHandler 封装 Worker 端的 HTTP 处理器
type WorkerHandler struct {
	execMgr *executor.Manager
}

// NewWorkerHandler 构造函数
func NewWorkerHandler(mgr *executor.Manager) *WorkerHandler {
	return &WorkerHandler{
		execMgr: mgr,
	}
}

// RegisterRoutes 注册路由到默认的 ServeMux
func (h *WorkerHandler) RegisterRoutes() {

	// 这些已经是 WorkerHandler 的方法 (需确保 terminal.go 和 log_stream.go 已更新)
	http.HandleFunc("/api/terminal/ws", h.HandleTerminal)
	http.HandleFunc("/api/log/ws", h.HandleLogStream)

	http.HandleFunc("/api/maintenance/cleanup_cache", h.handleCleanupCache)
	http.HandleFunc("/api/maintenance/scan_orphans", h.handleScanOrphans)
	http.HandleFunc("/api/maintenance/delete_orphans", h.handleDeleteOrphans)
}

// -------------------------------------------------------
// Handlers
// -------------------------------------------------------

// handleCleanupCache 清理缓存
func (h *WorkerHandler) handleCleanupCache(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}

	var req struct {
		Retain int `json:"retain"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}
	}

	result, err := h.execMgr.CleanupPackageCache(req.Retain)
	if err != nil {
		log.Printf("[Cleanup] Error: %v", err)
		http.Error(w, fmt.Sprintf("Cleanup failed: %v", err), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code": 0,
		"msg":  "success",
		"data": result,
	})
}

// handleScanOrphans 扫描孤儿
func (h *WorkerHandler) handleScanOrphans(w http.ResponseWriter, r *http.Request) {
	var req protocol.OrphanScanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", 400)
		return
	}

	sysMap := make(map[string]bool)
	for _, s := range req.ValidSystems {
		sysMap[s] = true
	}
	instMap := make(map[string]bool)
	for _, i := range req.ValidInstances {
		instMap[i] = true
	}

	items, err := h.execMgr.ScanOrphans(sysMap, instMap)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(protocol.OrphanScanResponse{Items: items})
}

// handleDeleteOrphans 删除孤儿
func (h *WorkerHandler) handleDeleteOrphans(w http.ResponseWriter, r *http.Request) {
	var req protocol.OrphanDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", 400)
		return
	}

	count, _ := h.execMgr.DeleteOrphans(req.Items)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"deleted_count": count})
}
