package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

var currentNodeID string

// InitNodeID 初始化节点 ID
// 1. 尝试从文件读取
// 2. 如果不存在，生成新的并写入文件
func InitNodeID(workDir string) (string, error) {
	idFile := filepath.Join(workDir, "node_id")

	// 1. 尝试读取
	if data, err := os.ReadFile(idFile); err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			currentNodeID = id
			return id, nil
		}
	}

	// 2. 生成新 ID
	newID := uuid.NewString()

	// 3. 写入文件
	if err := os.WriteFile(idFile, []byte(newID), 0644); err != nil {
		return "", fmt.Errorf("failed to save node_id: %v", err)
	}

	currentNodeID = newID
	return newID, nil
}

// GetNodeID 获取内存中的 ID
func GetNodeID() string {
	return currentNodeID
}
