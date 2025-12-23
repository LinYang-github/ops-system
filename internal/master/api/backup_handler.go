package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// handleListBackups 获取列表
func handleListBackups(w http.ResponseWriter, r *http.Request) {
	list, err := backupManager.ListBackups()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// handleCreateBackup 创建备份
func handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "405", 405)
		return
	}

	var req struct {
		WithFiles bool `json:"with_files"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if err := backupManager.CreateBackup(req.WithFiles); err != nil {
		http.Error(w, fmt.Sprintf("Backup failed: %v", err), 500)
		return
	}
	w.Write([]byte(`{"status":"ok"}`))
}

// handleDeleteBackup 删除备份
func handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Filename string `json:"filename"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if err := backupManager.DeleteBackup(req.Filename); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Write([]byte(`{"status":"ok"}`))
}

// handleRestoreBackup 恢复备份
func handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Filename string `json:"filename"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	// 注意：成功恢复后进程会退出，前端会收到网络错误或连接断开
	if err := backupManager.RestoreBackup(req.Filename); err != nil {
		http.Error(w, fmt.Sprintf("Restore failed: %v", err), 500)
		return
	}
	// 理论上执行不到这里，因为进程退出了
	w.Write([]byte(`{"status":"restoring"}`))
}
