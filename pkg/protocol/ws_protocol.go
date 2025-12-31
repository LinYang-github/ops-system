package protocol

import "encoding/json"

// 消息类型枚举
const (
	TypeHeartbeat     = "heartbeat" // Worker -> Master
	TypeRegister      = "register"  // Worker -> Master (首包)
	TypeCommand       = "command"   // Master -> Worker (下发指令)
	TypeResponse      = "response"  // Worker -> Master (指令执行结果)
	TypeLog           = "log"       // Worker -> Master (日志流)
	TypeConfig        = "config"
	TypeStatusReport  = "status_report"
	TypeLogFiles      = "log_files"
	TypeCleanupCache  = "cleanup_cache"
	TypeScanOrphans   = "scan_orphans"
	TypeDeleteOrphans = "delete_orphans"
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

// [新增] 清理缓存请求
type CleanupCacheRequest struct {
	Retain int `json:"retain"` // 保留版本数
}

// [新增] 清理缓存响应
type CleanupCacheResponse struct {
	NodeID       string   `json:"node_id"`
	FreedBytes   int64    `json:"freed_bytes"`
	DeletedFiles []string `json:"deleted_files"`
	Error        string   `json:"error,omitempty"`
}

// [新增] 孤儿扫描响应 (单节点)
type OrphanScanNodeResponse struct {
	NodeIP string       `json:"node_ip"`
	Items  []OrphanItem `json:"items"`
	Error  string       `json:"error,omitempty"`
}

// [新增] 孤儿删除请求 (单节点)
type OrphanDeleteRequestWorker struct {
	Items []string `json:"items"`
}

// [新增] 孤儿删除响应
type OrphanDeleteResponse struct {
	DeletedCount int    `json:"deleted_count"`
	Error        string `json:"error,omitempty"`
}
