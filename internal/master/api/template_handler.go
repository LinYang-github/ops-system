package api

import (
	"encoding/json"
	"net/http"

	"ops-system/pkg/code"
	"ops-system/pkg/e"
	"ops-system/pkg/response"
)

// ListTemplates 获取配置模板列表
// GET /api/templates
func (h *ServerHandler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	list, err := h.tplMgr.ListTemplates()
	if err != nil {
		response.Error(w, e.New(code.DatabaseError, "获取模板列表失败", err))
		return
	}
	response.Success(w, list)
}

// GetTemplate 获取单个模板详情
// GET /api/templates/detail?id=...
func (h *ServerHandler) GetTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		response.Error(w, e.New(code.ParamError, "缺少 id 参数", nil))
		return
	}

	tpl, err := h.tplMgr.GetTemplate(id)
	if err != nil {
		response.Error(w, e.New(code.DatabaseError, "获取模板详情失败", err))
		return
	}
	response.Success(w, tpl)
}

// CreateTemplate 创建配置模板
// POST /api/templates/create
func (h *ServerHandler) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, e.New(code.MethodNotAllowed, "Method not allowed", nil))
		return
	}

	var req struct {
		Name    string `json:"name"`
		Content string `json:"content"`
		Format  string `json:"format"` // yaml, json, properties, ini, etc.
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	if req.Name == "" {
		response.Error(w, e.New(code.ParamError, "模板名称不能为空", nil))
		return
	}

	if err := h.tplMgr.CreateTemplate(req.Name, req.Content, req.Format); err != nil {
		response.Error(w, e.New(code.DatabaseError, "创建模板失败", err))
		return
	}

	response.Success(w, nil)
}

// UpdateTemplate 更新配置模板
// POST /api/templates/update
func (h *ServerHandler) UpdateTemplate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, e.New(code.MethodNotAllowed, "Method not allowed", nil))
		return
	}

	var req struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Content string `json:"content"`
		Format  string `json:"format"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	if req.ID == "" || req.Name == "" {
		response.Error(w, e.New(code.ParamError, "ID或名称不能为空", nil))
		return
	}

	if err := h.tplMgr.UpdateTemplate(req.ID, req.Name, req.Content, req.Format); err != nil {
		response.Error(w, e.New(code.DatabaseError, "更新模板失败", err))
		return
	}

	response.Success(w, nil)
}

// DeleteTemplate 删除配置模板
// POST /api/templates/delete
func (h *ServerHandler) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, e.New(code.MethodNotAllowed, "Method not allowed", nil))
		return
	}

	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	if req.ID == "" {
		response.Error(w, e.New(code.ParamError, "ID不能为空", nil))
		return
	}

	if err := h.tplMgr.DeleteTemplate(req.ID); err != nil {
		response.Error(w, e.New(code.DatabaseError, "删除模板失败", err))
		return
	}

	response.Success(w, nil)
}
