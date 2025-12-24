package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"ops-system/internal/master/ws"

	"ops-system/pkg/response"
	"ops-system/pkg/utils"
)

// 辅助方法：广播更新 (现在作为 Handler 的私有方法)
func (h *ServerHandler) broadcastUpdate() {
	data := h.sysMgr.GetFullView(h.instMgr)
	ws.BroadcastSystems(data)
}

// handleGetSystems 获取系统列表及实例
func (h *ServerHandler) GetSystems(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	// 调用 sysManager 并传入 instManager 以合并数据
	response.Success(w, h.sysMgr.GetFullView(h.instMgr))
}

// handleCreateSystem 创建系统
func (h *ServerHandler) CreateSystem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, err)
		return
	}
	sys := h.sysMgr.CreateSystem(req.Name, req.Description)

	h.logMgr.RecordLog(utils.GetClientIP(r), "create_system", "system", req.Name, req.Description, "success")
	h.broadcastUpdate()

	response.Success(w, sys)
}

// handleDeleteSystem 删除系统
func (h *ServerHandler) DeleteSystem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, err)
		return
	}

	// 需要传入 instManager 以清理缓存
	if err := h.sysMgr.DeleteSystem(req.ID, h.instMgr); err != nil {
		response.Error(w, err)
		return
	}

	h.logMgr.RecordLog(utils.GetClientIP(r), "delete_system", "system", req.ID, "", "success")
	h.broadcastUpdate()

	response.Success(w, nil)
}

// handleCreateSystemModule 添加服务定义
func (h *ServerHandler) CreateSystemModule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SystemID       string `json:"system_id"`
		ModuleName     string `json:"module_name"`
		PackageName    string `json:"package_name"`
		PackageVersion string `json:"package_version"`
		Description    string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, err)
		return
	}

	if err := h.sysMgr.AddModule(req.SystemID, req.ModuleName, req.PackageName, req.PackageVersion, req.Description); err != nil {
		response.Error(w, err)
		return
	}

	detail := fmt.Sprintf("Pkg: %s v%s", req.PackageName, req.PackageVersion)
	h.logMgr.RecordLog(utils.GetClientIP(r), "add_module", "module", req.ModuleName, detail, "success")
	h.broadcastUpdate()

	response.Success(w, nil)
}

// handleDeleteSystemModule 删除服务定义
func (h *ServerHandler) DeleteSystemModule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, err)
		return
	}
	if err := h.sysMgr.DeleteModule(req.ID); err != nil {
		response.Error(w, err)
		return
	}

	h.logMgr.RecordLog(utils.GetClientIP(r), "delete_module", "module", req.ID, "", "success")
	h.broadcastUpdate()

	response.Success(w, nil)
}
