package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"ops-system/pkg/code"
	"ops-system/pkg/e"
	"ops-system/pkg/response"
)

// ListBackups 获取备份列表
// GET /api/backups
func (h *ServerHandler) ListBackups(w http.ResponseWriter, r *http.Request) {
	list, err := h.backupMgr.ListBackups()
	if err != nil {
		response.Error(w, e.New(code.ServerError, "获取备份列表失败", err))
		return
	}
	response.Success(w, list)
}

// CreateBackup 创建备份
// POST /api/backups/create
func (h *ServerHandler) CreateBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, e.New(code.MethodNotAllowed, "Method not allowed", nil))
		return
	}

	var req struct {
		WithFiles bool `json:"with_files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	if err := h.backupMgr.CreateBackup(req.WithFiles); err != nil {
		response.Error(w, e.New(code.ServerError, fmt.Sprintf("备份创建失败: %v", err), err))
		return
	}

	response.Success(w, nil)
}

// DeleteBackup 删除备份
// POST /api/backups/delete
func (h *ServerHandler) DeleteBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, e.New(code.MethodNotAllowed, "Method not allowed", nil))
		return
	}

	var req struct {
		Filename string `json:"filename"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	if err := h.backupMgr.DeleteBackup(req.Filename); err != nil {
		response.Error(w, e.New(code.ServerError, "删除备份文件失败", err))
		return
	}

	response.Success(w, nil)
}

// RestoreBackup 恢复备份 (危险操作)
// POST /api/backups/restore
func (h *ServerHandler) RestoreBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, e.New(code.MethodNotAllowed, "Method not allowed", nil))
		return
	}

	var req struct {
		Filename string `json:"filename"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	// 注意：成功恢复后进程会退出，前端可能收不到完整的响应
	// 但我们还是尝试写入 response
	if err := h.backupMgr.RestoreBackup(req.Filename); err != nil {
		response.Error(w, e.New(code.ServerError, fmt.Sprintf("恢复失败: %v", err), err))
		return
	}

	// 理论上执行不到这里，因为 backupMgr.RestoreBackup 最后会 os.Exit(0)
	response.Success(w, map[string]string{"status": "restoring"})
}
