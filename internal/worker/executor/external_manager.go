package executor

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"ops-system/pkg/protocol"
)

// RegisterExternal 注册纳管实例
// 将 ExternalConfig 转换为统一的 ServiceManifest 并保存为 service.json
func RegisterExternal(req protocol.RegisterExternalRequest) error {
	if baseWorkDir == "" {
		return fmt.Errorf("executor not initialized")
	}

	// 1. 创建虚拟目录: instances/external/{SystemName}/{InstanceID}/
	virtualDir := filepath.Join(baseWorkDir, "external", req.SystemName, req.InstanceID)
	log.Printf("[External] Registering: %s", virtualDir)

	if err := os.MkdirAll(virtualDir, 0755); err != nil {
		return err
	}

	// 2. 转换配置为 ServiceManifest
	// 解析启动命令 "cmd arg1 arg2"
	startBin, startArgs := parseCmdString(req.Config.StartCmd)
	stopBin, stopArgs := parseCmdString(req.Config.StopCmd)

	manifest := protocol.ServiceManifest{
		Name:    req.Config.Name,
		Version: "external",

		// 核心转换
		IsExternal:      true,
		ExternalWorkDir: req.Config.WorkDir,
		Entrypoint:      startBin,
		Args:            startArgs,
		StopEntrypoint:  stopBin,
		StopArgs:        stopArgs,
		PidStrategy:     req.Config.PidStrategy,
		ProcessName:     req.Config.ProcessName,

		Description: "纳管外部服务",
	}

	// 3. 保存为 service.json (统一文件名)
	configPath := filepath.Join(virtualDir, "service.json")
	file, err := os.Create(configPath)
	if err != nil {
		return err
	}
	defer file.Close()

	return json.NewEncoder(file).Encode(manifest)
}

// 辅助：简单的命令行解析 (不支持复杂的引号转义，仅按空格分割)
func parseCmdString(cmdStr string) (string, []string) {
	if cmdStr == "" {
		return "", nil
	}
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return "", nil
	}
	return parts[0], parts[1:]
}
