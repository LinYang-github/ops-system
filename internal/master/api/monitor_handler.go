package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"ops-system/internal/master/monitor"
)

// handleQueryRange 模拟 Prometheus 查询接口
// GET /api/monitor/query_range?query=node_cpu_usage&instance=1.2.3.4&start=...&end=...
func (h *ServerHandler) QueryRange(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	metric := q.Get("query")   // e.g. node_cpu_usage
	ip := q.Get("instance")    // e.g. 192.168.1.10
	startStr := q.Get("start") // Unix Timestamp
	endStr := q.Get("end")

	if metric == "" || ip == "" {
		http.Error(w, "missing query or instance", 400)
		return
	}

	// 默认查询最近 10 分钟
	now := time.Now().Unix()
	start := now - 600
	end := now

	if s, err := strconv.ParseInt(startStr, 10, 64); err == nil {
		start = s
	}
	if e, err := strconv.ParseInt(endStr, 10, 64); err == nil {
		end = e
	}

	// 1. 查询数据
	points := h.monitorStore.QueryRange(metric, ip, start, end)

	// 2. 格式化为 Prometheus 结构
	resp := monitor.FormatPrometheusResponse(metric, ip, points)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
