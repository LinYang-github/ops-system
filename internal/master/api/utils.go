package api

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"
)

// calculateFileHash 计算文件 SHA256 (API 包内部通用)
func calculateFileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// containsIgnoreCase 忽略大小写包含判断
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
