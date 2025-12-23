package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const LogKeyConsole = "Console Log"

// GetLogFiles 获取指定实例支持的所有日志名称
func GetLogFiles(instID string) ([]string, error) {
	// 1. 找到实例目录
	workDir, found := FindInstanceDir(instID)
	if !found {
		return nil, fmt.Errorf("instance dir not found")
	}

	// 2. 读取配置
	m, err := readManifest(workDir)
	if err != nil {
		// 如果读不到配置，至少返回 Console Log
		return []string{LogKeyConsole}, nil
	}

	// 3. 组装列表
	files := []string{LogKeyConsole} // 默认包含控制台日志

	// 提取 LogPaths 中的 Key
	var customKeys []string
	for k := range m.LogPaths {
		customKeys = append(customKeys, k)
	}

	// 排序，保证前端显示顺序稳定
	sort.Strings(customKeys)
	files = append(files, customKeys...)

	return files, nil
}

// GetLogPath 根据日志名称获取物理文件绝对路径
func GetLogPath(instID string, logKey string) (string, error) {
	workDir, found := FindInstanceDir(instID)
	if !found {
		return "", fmt.Errorf("instance dir not found")
	}

	// 1. 默认控制台日志
	if logKey == LogKeyConsole || logKey == "" {
		return filepath.Join(workDir, "app.log"), nil
	}

	// 2. 读取配置查找自定义日志
	m, err := readManifest(workDir)
	if err != nil {
		return "", fmt.Errorf("manifest read failed: %v", err)
	}

	path, ok := m.LogPaths[logKey]
	if !ok {
		return "", fmt.Errorf("log key '%s' not found in config", logKey)
	}

	// 3. 路径解析
	// 如果是绝对路径，直接使用
	if filepath.IsAbs(path) {
		return path, nil
	}

	// 如果是相对路径，需要区分是否为纳管服务
	baseDir := workDir // 默认基于实例目录
	if m.IsExternal && m.ExternalWorkDir != "" {
		baseDir = m.ExternalWorkDir // 纳管服务基于其实际工作目录
	}

	fullPath := filepath.Join(baseDir, path)

	// (可选) 检查文件是否存在
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		// 这里不返回错误，允许文件暂时不存在（比如还没生成），由 Tail 等待
		// 但为了调试方便，可以打个日志
	}

	return fullPath, nil
}
