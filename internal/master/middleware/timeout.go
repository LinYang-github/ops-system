package middleware

import (
	"log"
	"net/http"
	"strings"
	"time"
)

// TimeoutMiddleware 针对 RESTful API 设置超时
// timeout: 超时时长
func TimeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		// 【新增】启动时检查：防止传入 nil handler 导致运行时 Panic
		if next == nil {
			log.Fatal("❌ [TimeoutMiddleware] Setup Error: 'next' handler is nil! Check your server.go middleware chaining.")
		}
		// 使用标准库的 TimeoutHandler
		// 当超时发生时，返回 503 Service Unavailable 和指定消息
		timeoutHandler := http.TimeoutHandler(next, timeout, `{"code": 504, "msg": "request timeout"}`)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path

			// ====================================================
			// 【白名单】以下路径不做超时限制 (长连接/流式传输)
			// ====================================================

			// 1. WebSocket 相关
			if strings.HasPrefix(path, "/api/ws") ||
				strings.HasPrefix(path, "/api/log/ws") ||
				strings.HasPrefix(path, "/api/node/terminal") ||
				strings.HasPrefix(path, "/api/instance/logs/stream") ||
				strings.HasPrefix(path, "/api/upload") ||
				strings.HasPrefix(path, "/download/") ||
				strings.HasPrefix(path, "/api/systems/export") {
				next.ServeHTTP(w, r)
				return
			}

			// 2. 文件上传 (流式直传)
			if strings.HasPrefix(path, "/api/upload/direct") ||
				strings.HasPrefix(path, "/api/upload") { // 包含普通上传
				next.ServeHTTP(w, r)
				return
			}

			// 3. 文件下载 (静态资源)
			if strings.HasPrefix(path, "/download/") {
				next.ServeHTTP(w, r)
				return
			}

			// 4. (可选) 如果有些耗时的批量操作接口需要更长时间，也可以单独放行
			// if strings.HasPrefix(path, "/api/systems/action") { ... }

			// ====================================================
			// 普通 API 走超时控制
			// ====================================================
			timeoutHandler.ServeHTTP(w, r)
		})
	}
}
