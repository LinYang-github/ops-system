package protocol

import "encoding/json"

// 消息类型枚举
const (
	TypeHeartbeat    = "heartbeat" // Worker -> Master
	TypeRegister     = "register"  // Worker -> Master (首包)
	TypeCommand      = "command"   // Master -> Worker (下发指令)
	TypeResponse     = "response"  // Worker -> Master (指令执行结果)
	TypeLog          = "log"       // Worker -> Master (日志流)
	TypeConfig       = "config"
	TypeStatusReport = "status_report"
	TypeLogFiles     = "log_files"
)

// WSMessage 统一信封
type WSMessage struct {
	Type    string          `json:"type"`
	Id      string          `json:"id"` // 请求ID (用于关联 Request/Response)
	Payload json.RawMessage `json:"payload"`
}

// 获取日志文件请求体
type LogFilesRequest struct {
	InstanceID string `json:"instance_id"`
}

// 辅助方法：创建消息
func NewWSMessage(msgType string, id string, data interface{}) (*WSMessage, error) {
	bytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return &WSMessage{
		Type:    msgType,
		Id:      id,
		Payload: bytes,
	}, nil
}
