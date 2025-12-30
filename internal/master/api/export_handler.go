package api

import (
	"fmt"
	"net/http"
	"time"

	"ops-system/pkg/code"
	"ops-system/pkg/e"
	"ops-system/pkg/response"
)

// ExportSystem 导出系统
// GET /api/systems/export?id=...&os=windows|linux
func (h *ServerHandler) ExportSystem(w http.ResponseWriter, r *http.Request) {
	sysID := r.URL.Query().Get("id")
	targetOS := r.URL.Query().Get("os")
	if sysID == "" {
		response.Error(w, e.New(code.ParamError, "missing id", nil))
		return
	}
	if targetOS == "" {
		targetOS = "linux"
	}

	// 设置响应头，触发下载
	filename := fmt.Sprintf("export_%s_%d.zip", sysID, time.Now().Unix())
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.Header().Set("Content-Type", "application/zip")

	// 直接写入 ResponseWriter
	if err := h.exportMgr.ExportSystem(sysID, w, targetOS); err != nil {
		// 注意：如果已经开始写入流，这里再报错前端可能只收到截断的文件
		// 生产环境建议先写入临时文件再 ServeFile，但为了流式节省内存，这里直接写
		// 可以在日志里记录错误
		fmt.Printf("Export error: %v\n", err)
		return
	}
}
