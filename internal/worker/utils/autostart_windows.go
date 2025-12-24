package utils

import (
	"fmt"
	"os/exec"
	"strings"
)

func enableAutoStart(exePath string, args []string) error {
	// 构造命令: schtasks /Create /TN "ops-worker" /TR "'C:\path\worker.exe' -master=..." /SC ONSTART /RU SYSTEM /F

	// 注意 Windows 路径和引号的处理
	cmdLine := fmt.Sprintf(`"%s" %s`, exePath, strings.Join(args, " "))

	// /SC ONSTART: 开机启动
	// /RU SYSTEM: 以系统权限运行(后台服务)
	// /RL HIGHEST: 最高权限
	// /F: 强制覆盖
	cmd := exec.Command("schtasks", "/Create", "/TN", ServiceName, "/TR", cmdLine, "/SC", "ONSTART", "/RU", "SYSTEM", "/RL", "HIGHEST", "/F")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks failed: %v, output: %s", err, string(output))
	}

	fmt.Println("✅ Windows 计划任务已创建 (开机自启)")
	fmt.Println("注意：该任务将在下次重启后生效，或手动在任务计划程序中启动。")
	return nil
}

func disableAutoStart() error {
	cmd := exec.Command("schtasks", "/Delete", "/TN", ServiceName, "/F")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("delete task failed: %v, output: %s", err, string(output))
	}
	fmt.Println("✅ Windows 计划任务已移除")
	return nil
}
