package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"ops-system/pkg/utils"
)

// handleGetSystems 获取系统列表及实例
func handleGetSystems(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	// 调用 sysManager 并传入 instManager 以合并数据
	json.NewEncoder(w).Encode(sysManager.GetFullView(instManager))
}

// handleCreateSystem 创建系统
func handleCreateSystem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	sys := sysManager.CreateSystem(req.Name, req.Description)

	logManager.RecordLog(utils.GetClientIP(r), "create_system", "system", req.Name, req.Description, "success")
	broadcastUpdate()

	json.NewEncoder(w).Encode(sys)
}

// handleDeleteSystem 删除系统
func handleDeleteSystem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	// 需要传入 instManager 以清理缓存
	if err := sysManager.DeleteSystem(req.ID, instManager); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	logManager.RecordLog(utils.GetClientIP(r), "delete_system", "system", req.ID, "", "success")
	broadcastUpdate()

	w.Write([]byte(`{"status":"ok"}`))
}

// handleCreateSystemModule 添加服务定义
func handleCreateSystemModule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SystemID       string `json:"system_id"`
		ModuleName     string `json:"module_name"`
		PackageName    string `json:"package_name"`
		PackageVersion string `json:"package_version"`
		Description    string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	if err := sysManager.AddModule(req.SystemID, req.ModuleName, req.PackageName, req.PackageVersion, req.Description); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	detail := fmt.Sprintf("Pkg: %s v%s", req.PackageName, req.PackageVersion)
	logManager.RecordLog(utils.GetClientIP(r), "add_module", "module", req.ModuleName, detail, "success")
	broadcastUpdate()

	w.Write([]byte(`{"status":"ok"}`))
}

// handleDeleteSystemModule 删除服务定义
func handleDeleteSystemModule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := sysManager.DeleteModule(req.ID); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	logManager.RecordLog(utils.GetClientIP(r), "delete_module", "module", req.ID, "", "success")
	broadcastUpdate()

	w.Write([]byte(`{"status":"ok"}`))
}
