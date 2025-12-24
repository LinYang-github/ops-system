package api

import (
	"net/http"
	"strconv"
	"time"

	"ops-system/internal/master/monitor"
	"ops-system/pkg/code"
	"ops-system/pkg/e"
	"ops-system/pkg/response"
)

// QueryRange 模拟 Prometheus 查询接口
// GET /api/monitor/query_range?query=node_cpu_usage&instance=1.2.3.4&start=...&end=...
func (h *ServerHandler) QueryRange(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	metric := q.Get("query")   // e.g. node_cpu_usage
	ip := q.Get("instance")    // e.g. 192.168.1.10
	startStr := q.Get("start") // Unix Timestamp
	endStr := q.Get("end")

	if metric == "" || ip == "" {
		response.Error(w, e.New(code.ParamError, "缺少 query 或 instance 参数", nil))
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

	// 1. 查询数据 (使用注入的 monitorStore)
	points := h.monitorStore.QueryRange(metric, ip, start, end)

	// 2. 格式化为 Prometheus 结构
	// 返回结构: { status: "success", data: { resultType: "matrix", result: [...] } }
	promResp := monitor.FormatPrometheusResponse(metric, ip, points)

	// 3. 统一响应
	// 最终前端收到的 JSON: { code: 0, msg: "success", data: { status: "success", data: ... } }
	// 前端 request.js 拦截器解包后，组件拿到的就是 promResp
	response.Success(w, promResp)
}

// 这是一个预留的 Handler，如果以后需要暴露 metrics 给真正的 Prometheus Server 抓取
// GET /metrics
func (h *ServerHandler) Metrics(w http.ResponseWriter, r *http.Request) {
	// 这里通常使用 prometheus/client_golang 的 promhttp.Handler()
	// 目前不需要实现，仅作架构占位说明
	w.WriteHeader(http.StatusNotImplemented)
}
