package agent

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// GetOrCreateNodeID 获取或生成持久化的 NodeID
func GetOrCreateNodeID(workDir string) (string, error) {
	// 确保目录存在
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return "", err
	}

	idFile := filepath.Join(workDir, "node_id")

	// 1. 尝试读取
	if data, err := os.ReadFile(idFile); err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			return id, nil
		}
	}

	// 2. 生成新 ID (UUID v4)
	newID := uuid.New().String()

	// 3. 持久化存储
	if err := os.WriteFile(idFile, []byte(newID), 0644); err != nil {
		return "", err
	}

	return newID, nil
}
