package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"ops-system/pkg/code"
	"ops-system/pkg/e"
	"ops-system/pkg/protocol"
	"ops-system/pkg/response"
)

// ListRules 获取所有告警规则
// GET /api/alerts/rules
func (h *ServerHandler) ListRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.alertMgr.GetRules()
	if err != nil {
		response.Error(w, e.New(code.DatabaseError, "获取规则列表失败", err))
		return
	}
	response.Success(w, rules)
}

// AddRule 添加告警规则
// POST /api/alerts/rules/add
func (h *ServerHandler) AddRule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, e.New(code.MethodNotAllowed, "Method not allowed", nil))
		return
	}

	var rule protocol.AlertRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	if err := h.alertMgr.AddRule(rule); err != nil {
		response.Error(w, e.New(code.AlertRuleError, "添加规则失败", err))
		return
	}

	response.Success(w, nil)
}

// DeleteRule 删除告警规则
// GET /api/alerts/rules/delete?id=...
func (h *ServerHandler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		response.Error(w, e.New(code.ParamError, "无效的规则ID", err))
		return
	}

	if err := h.alertMgr.DeleteRule(id); err != nil {
		response.Error(w, e.New(code.AlertRuleError, "删除规则失败", err))
		return
	}

	response.Success(w, nil)
}

// GetAlerts 获取告警事件 (活跃 + 历史)
// GET /api/alerts/events
func (h *ServerHandler) GetAlerts(w http.ResponseWriter, r *http.Request) {
	active, err := h.alertMgr.GetActiveEvents()
	if err != nil {
		response.Error(w, e.New(code.DatabaseError, "获取活跃告警失败", err))
		return
	}

	history, err := h.alertMgr.GetHistoryEvents(50) // 获取最近50条历史
	if err != nil {
		response.Error(w, e.New(code.DatabaseError, "获取历史告警失败", err))
		return
	}

	type Resp struct {
		Active  []*protocol.AlertEvent `json:"active"`
		History []*protocol.AlertEvent `json:"history"`
	}

	response.Success(w, Resp{Active: active, History: history})
}

// DeleteEvent 删除单条告警记录
// POST /api/alerts/events/delete
func (h *ServerHandler) DeleteEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, e.New(code.MethodNotAllowed, "Method not allowed", nil))
		return
	}

	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	if err := h.alertMgr.DeleteEvent(req.ID); err != nil {
		response.Error(w, e.New(code.DatabaseError, "删除记录失败", err))
		return
	}

	response.Success(w, nil)
}

// ClearEvents 清空所有告警记录
// POST /api/alerts/events/clear
func (h *ServerHandler) ClearEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, e.New(code.MethodNotAllowed, "Method not allowed", nil))
		return
	}

	if err := h.alertMgr.ClearEvents(); err != nil {
		response.Error(w, e.New(code.DatabaseError, "清空记录失败", err))
		return
	}

	response.Success(w, nil)
}
