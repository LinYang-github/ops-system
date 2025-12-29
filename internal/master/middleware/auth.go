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
			path := r.URL.Path

			// 1. 放行静态资源 (非 /api 开头的请求)
			if !strings.HasPrefix(path, "/api/") {
				next.ServeHTTP(w, r)
				return
			}

			// 2. 放行白名单接口
			// - WebSocket 握手
			// - 直传接口 (模拟 Presigned URL，不带 Token，由 Key 自身保证上下文)
			if strings.HasPrefix(path, "/api/ws") ||
				strings.HasPrefix(path, "/api/log/ws") ||
				strings.HasPrefix(path, "/api/instance/logs/stream") ||
				strings.HasPrefix(path, "/api/node/terminal") ||
				// 【关键修复】放行本地直传接口
				strings.HasPrefix(path, "/api/upload/direct") ||
				strings.HasPrefix(path, "/api/login") {

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
