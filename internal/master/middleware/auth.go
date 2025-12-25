package middleware

import (
	"net/http"
	"strings"

	"ops-system/pkg/code"
	"ops-system/pkg/e"
	"ops-system/pkg/response"
)

// AuthMiddleware 鉴权中间件
func AuthMiddleware(secretKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. 放行静态资源 (非 /api 开头的请求)
			if !strings.HasPrefix(r.URL.Path, "/api/") {
				next.ServeHTTP(w, r)
				return
			}

			// 2. 放行 WebSocket (握手时无法带 Header，通常通过 Query 参数或 Cookie，这里暂且放行或改为 Query 校验)
			// 为了安全，建议 WebSocket 改为 Query 参数鉴权：ws://...?token=xxx
			if strings.HasPrefix(r.URL.Path, "/api/ws") || strings.HasPrefix(r.URL.Path, "/api/log/ws") || strings.HasPrefix(r.URL.Path, "/api/instance/logs/stream") {
				// 简单实现：检查 Query 参数 token (需要在前端 WS 连接时加上)
				// queryToken := r.URL.Query().Get("token")
				// if queryToken != secretKey { ... }

				// 暂时放行，由后续逻辑处理，或者你可以在这里加上 Query 参数校验
				next.ServeHTTP(w, r)
				return
			}

			// 3. 检查 Header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				response.Error(w, e.New(code.Unauthorized, "缺少认证 Token", nil))
				return
			}

			// 格式: "Bearer <token>"
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" || parts[1] != secretKey {
				response.Error(w, e.New(code.Unauthorized, "认证 Token 无效", nil))
				return
			}

			// 4. 校验通过
			next.ServeHTTP(w, r)
		})
	}
}
