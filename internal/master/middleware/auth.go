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

			// 2. 放行 WebSocket 相关路径
			// WebSocket 握手无法带 Header，需要放行或改用 Query 参数校验
			if strings.HasPrefix(r.URL.Path, "/api/ws") ||
				strings.HasPrefix(r.URL.Path, "/api/log/ws") ||
				strings.HasPrefix(r.URL.Path, "/api/instance/logs/stream") ||
				// 【核心修复】添加终端路径到白名单
				strings.HasPrefix(r.URL.Path, "/api/node/terminal") {

				// (可选优化：在这里校验 r.URL.Query().Get("token"))

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
