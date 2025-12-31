package executor

import (
	"fmt"
	"path/filepath"
	"sort"
)

const LogKeyConsole = "Console Log"

// GetLogFiles 获取指定实例支持的所有日志名称
func (m *Manager) GetLogFiles(instID string) ([]string, error) {
	// 1. 找到实例目录
	workDir, found := m.FindInstanceDir(instID)
	if !found {
		return nil, fmt.Errorf("instance dir not found")
	}

	// 2. 读取配置
	mf, err := readManifest(workDir)
	if err != nil {
		return []string{LogKeyConsole}, nil
	}

	// 3. 组装列表
	files := []string{LogKeyConsole}
	var customKeys []string
	for k := range mf.LogPaths {
		customKeys = append(customKeys, k)
	}
	sort.Strings(customKeys)
	files = append(files, customKeys...)

	return files, nil
}

// GetLogPath 根据日志名称获取物理文件绝对路径
func (m *Manager) GetLogPath(instID string, logKey string) (string, error) {
	workDir, found := m.FindInstanceDir(instID)
	if !found {
		return "", fmt.Errorf("instance dir not found")
	}

	// 1. 默认控制台日志
	if logKey == LogKeyConsole || logKey == "" {
		return filepath.Join(workDir, "app.log"), nil
	}

	// 2. 读取配置
	mf, err := readManifest(workDir)
	if err != nil {
		return "", fmt.Errorf("manifest read failed: %v", err)
	}

	path, ok := mf.LogPaths[logKey]
	if !ok {
		return "", fmt.Errorf("log key '%s' not found in config", logKey)
	}

	// 3. 路径解析
	if filepath.IsAbs(path) {
		return path, nil
	}

	baseDir := workDir
	if mf.IsExternal && mf.ExternalWorkDir != "" {
		baseDir = mf.ExternalWorkDir
	}

	fullPath := filepath.Join(baseDir, path)
	return fullPath, nil
}
