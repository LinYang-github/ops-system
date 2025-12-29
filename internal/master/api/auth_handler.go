package api

import (
	"encoding/json"
	"net/http"

	"ops-system/pkg/code"
	"ops-system/pkg/e"
	"ops-system/pkg/response"
)

// HandleLogin 模拟登录
// POST /api/login
func (h *ServerHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, e.New(code.MethodNotAllowed, "Method not allowed", nil))
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	// 模拟鉴权逻辑 (未来可对接数据库或 LDAP)
	// 这里假设内置管理员账号 admin / 123456
	if req.Username == "admin" && req.Password == "123456" {
		// 登录成功，返回 SecretKey 作为 Token
		// 未来这里可以颁发 JWT
		data := map[string]string{
			"token": h.secretKey,
		}
		response.Success(w, data)
	} else {
		response.Error(w, e.New(code.Unauthorized, "用户名或密码错误", nil))
	}
}
