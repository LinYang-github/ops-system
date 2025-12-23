package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"ops-system/pkg/protocol"
)

// 全局变量 alertManager (在 server.go 定义)

func handleListRules(w http.ResponseWriter, r *http.Request) {
	rules, _ := alertManager.GetRules()
	json.NewEncoder(w).Encode(rules)
}

func handleAddRule(w http.ResponseWriter, r *http.Request) {
	var rule protocol.AlertRule
	json.NewDecoder(r.Body).Decode(&rule)
	alertManager.AddRule(rule)
	w.Write([]byte(`{"status":"ok"}`))
}

func handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	id, _ := strconv.ParseInt(idStr, 10, 64)
	alertManager.DeleteRule(id)
	w.Write([]byte(`{"status":"ok"}`))
}

func handleGetAlerts(w http.ResponseWriter, r *http.Request) {
	type Resp struct {
		Active  []*protocol.AlertEvent `json:"active"`
		History []*protocol.AlertEvent `json:"history"`
	}
	active, _ := alertManager.GetActiveEvents()
	history, _ := alertManager.GetHistoryEvents(50)

	json.NewEncoder(w).Encode(Resp{Active: active, History: history})
}

// handleDeleteEvent 删除单个事件
func handleDeleteEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", 400)
		return
	}

	if err := alertManager.DeleteEvent(req.ID); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Write([]byte(`{"status":"ok"}`))
}

// handleClearEvents 清空所有事件
func handleClearEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}

	// 这里可以加一个简单的鉴权或参数校验，防止误调
	// 暂时直接执行
	if err := alertManager.ClearEvents(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Write([]byte(`{"status":"ok"}`))
}
