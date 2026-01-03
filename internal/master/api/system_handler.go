package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"ops-system/internal/master/ws"

	"ops-system/pkg/code"
	"ops-system/pkg/e"
	"ops-system/pkg/protocol"
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

// CreateSystemModule 添加服务定义
// POST /api/systems/module/add
func (h *ServerHandler) CreateSystemModule(w http.ResponseWriter, r *http.Request) {
	// 【修改点1】直接解析为 protocol.SystemModule 结构体
	// 这样可以自动映射 start_order, readiness_type 等新字段
	var req protocol.SystemModule
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	// 【修改点2】设置默认值
	if req.StartOrder <= 0 {
		req.StartOrder = 1 // 默认为优先级 1
	}

	// 【修改点3】调用 Manager (Manager.AddModule 的签名也需要同步修改为接收 struct)
	if err := h.sysMgr.AddModule(req); err != nil {
		response.Error(w, e.New(code.DatabaseError, "添加组件失败", err))
		return
	}

	// 记录日志 (增加 Order 信息)
	detail := fmt.Sprintf("Pkg: %s v%s, Order: %d", req.PackageName, req.PackageVersion, req.StartOrder)
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

// UpdateSystemModule 更新服务定义
// POST /api/systems/module/update
func (h *ServerHandler) UpdateSystemModule(w http.ResponseWriter, r *http.Request) {
	var req protocol.SystemModule
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	if req.ID == "" {
		response.Error(w, e.New(code.ParamError, "缺少模块ID", nil))
		return
	}

	if err := h.sysMgr.UpdateModule(req); err != nil {
		response.Error(w, e.New(code.DatabaseError, "更新组件失败", err))
		return
	}

	h.logMgr.RecordLog(utils.GetClientIP(r), "update_module", "module", req.ModuleName, "Updated params", "success")
	h.broadcastUpdate()
	response.Success(w, nil)
}
