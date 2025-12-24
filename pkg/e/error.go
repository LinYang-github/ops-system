package e

import "fmt"

// CodeError 包含错误码的自定义错误
type CodeError struct {
	Code int
	Msg  string
	Raw  error // 原始错误，用于后端日志记录，不展示给前端
}

func (e *CodeError) Error() string {
	if e.Raw != nil {
		return fmt.Sprintf("%s: %v", e.Msg, e.Raw)
	}
	return e.Msg
}

// New 创建一个新的业务错误
func New(code int, msg string, raw error) *CodeError {
	return &CodeError{
		Code: code,
		Msg:  msg,
		Raw:  raw,
	}
}
