package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"runtime"

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
	http.HandleFunc("/api/exec", h.handleExec)

	// 这些已经是 WorkerHandler 的方法 (需确保 terminal.go 和 log_stream.go 已更新)
	http.HandleFunc("/api/terminal/ws", h.HandleTerminal)
	http.HandleFunc("/api/log/ws", h.HandleLogStream)

	http.HandleFunc("/api/deploy", h.handleDeploy)
	http.HandleFunc("/api/instance/action", h.handleInstanceAction)
	http.HandleFunc("/api/external/register", h.handleRegisterExternal)
	http.HandleFunc("/api/maintenance/cleanup_cache", h.handleCleanupCache)
	http.HandleFunc("/api/maintenance/scan_orphans", h.handleScanOrphans)
	http.HandleFunc("/api/maintenance/delete_orphans", h.handleDeleteOrphans)
	http.HandleFunc("/api/log/files", h.handleGetLogFiles)
}

// StartWorkerServer 启动 HTTP 服务
// 保留此函数以兼容部分旧调用，但主要逻辑在 main.go 中直接 ListenAndServe
func StartWorkerServer(addr string) {
	log.Printf("Worker HTTP Server started on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Worker HTTP Server failed: %v", err)
	}
}

// -------------------------------------------------------
// Handlers
// -------------------------------------------------------

// handleRegisterExternal 处理纳管服务注册
func (h *WorkerHandler) handleRegisterExternal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}

	var req protocol.RegisterExternalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", 400)
		return
	}

	if err := h.execMgr.RegisterExternal(req); err != nil {
		log.Printf("[External] Register failed: %v", err)
		http.Error(w, err.Error(), 500)
		return
	}

	log.Printf("[External] Registered successfully: %s", req.Config.Name)
	w.Write([]byte(`{"status":"ok"}`))
}

// handleDeploy 部署实例 (异步化)
func (h *WorkerHandler) handleDeploy(w http.ResponseWriter, r *http.Request) {
	var req protocol.DeployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))

	go func() {
		h.execMgr.ReportStatus(req.InstanceID, "deploying", 0, 0)

		if err := h.execMgr.DeployInstance(req); err != nil {
			log.Printf("[Deploy Error] %v", err)
			h.execMgr.ReportStatus(req.InstanceID, "error", 0, 0)
		} else {
			log.Printf("[Deploy Success] %s", req.InstanceID)
			h.execMgr.ReportStatus(req.InstanceID, "stopped", 0, 0)
		}
	}()
}

// handleInstanceAction 处理实例启停
func (h *WorkerHandler) handleInstanceAction(w http.ResponseWriter, r *http.Request) {
	var req protocol.InstanceActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	var status string
	var pid int
	var uptime int64
	var execErr error

	workDir, found := h.execMgr.FindInstanceDir(req.InstanceID)

	if !found && req.Action != "destroy" {
		execErr = fmt.Errorf("instance dir not found for ID: %s", req.InstanceID)
	} else {
		switch req.Action {
		case "start":
			result := h.execMgr.StartProcess(workDir)
			status = result.Status
			pid = result.PID
			uptime = result.Uptime
			execErr = result.Error
		case "stop":
			status, pid, execErr = h.execMgr.StopProcess(workDir)
		case "destroy":
			execErr = h.execMgr.HandleAction(req)
			status = "destroyed"
			pid = 0
			uptime = 0
		default:
			execErr = fmt.Errorf("unsupported action: %s", req.Action)
		}
	}

	if execErr != nil {
		http.Error(w, execErr.Error(), 500)
		return
	}

	h.execMgr.ReportStatus(req.InstanceID, status, pid, uptime)
	w.Write([]byte(`{"status":"ok"}`))
}

// handleExec 处理 CMD 命令
func (h *WorkerHandler) handleExec(w http.ResponseWriter, r *http.Request) {
	var req protocol.CommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", req.Command)
	} else {
		cmd = exec.Command("sh", "-c", req.Command)
	}

	output, err := cmd.CombinedOutput()
	result := map[string]string{
		"output": string(output),
		"error":  "",
	}
	if err != nil {
		result["error"] = err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleGetLogFiles 获取日志列表
func (h *WorkerHandler) handleGetLogFiles(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("instance_id")
	if id == "" {
		http.Error(w, "missing instance_id", 400)
		return
	}

	files, err := h.execMgr.GetLogFiles(id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	resp := protocol.LogFilesResp{
		InstanceID: id,
		Files:      files,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

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
