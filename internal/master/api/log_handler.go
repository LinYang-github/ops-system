package api

import (
	"encoding/json"
	"net/http"
	"ops-system/pkg/protocol"
)

func handleGetOpLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "405", 405)
		return
	}
	var req protocol.LogQueryReq
	json.NewDecoder(r.Body).Decode(&req)

	// 使用 logManager
	logs, err := logManager.GetLogs(req.Page, req.PageSize, req.Keyword)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	json.NewEncoder(w).Encode(logs)
}
