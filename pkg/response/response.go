package response

import (
	"encoding/json"
	"log"
	"net/http"

	"ops-system/pkg/code"
	"ops-system/pkg/e"
)

type Response struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data"` // data 字段可以是 null, object, array
}

// Result 基础响应方法
func Result(w http.ResponseWriter, httpStatus int, bizCode int, msg string, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)

	resp := Response{
		Code: bizCode,
		Msg:  msg,
		Data: data,
	}
	json.NewEncoder(w).Encode(resp)
}

// Success 成功响应 (HTTP 200)
func Success(w http.ResponseWriter, data interface{}) {
	Result(w, http.StatusOK, code.Success, "success", data)
}

// Error 错误响应 (HTTP 200, 业务错误码非0)
func Error(w http.ResponseWriter, err error) {
	// 1. 如果是自定义业务错误
	if bizErr, ok := err.(*e.CodeError); ok {
		// 记录原始错误日志（如果有）
		if bizErr.Raw != nil {
			log.Printf("[BizError] %s | Raw: %v", bizErr.Msg, bizErr.Raw)
		}
		Result(w, http.StatusOK, bizErr.Code, bizErr.Msg, nil)
		return
	}

	// 2. 如果是普通系统错误
	log.Printf("[SysError] %v", err)
	Result(w, http.StatusOK, code.ServerError, "系统内部错误: "+err.Error(), nil)
}
