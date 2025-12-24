package utils

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const unitContent = `[Unit]
Description=Ops System Worker Agent
After=network.target

[Service]
Type=simple
ExecStart=%s %s
Restart=always
RestartSec=5
WorkingDirectory=%s
User=root

[Install]
WantedBy=multi-user.target
`

func enableAutoStart(exePath string, args []string) error {
	serviceFile := fmt.Sprintf("/etc/systemd/system/%s.service", ServiceName)

	// 1. 生成 .service 文件
	// args 拼接
	argsStr := strings.Join(args, " ")
	workDir := filepath.Dir(exePath) // 默认在 exe 目录下运行

	content := fmt.Sprintf(unitContent, exePath, argsStr, workDir)

	if err := os.WriteFile(serviceFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("write service file failed (need root?): %v", err)
	}

	// 2. 重新加载并启用
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return err
	}
	if err := exec.Command("systemctl", "enable", ServiceName).Run(); err != nil {
		return err
	}
	if err := exec.Command("systemctl", "start", ServiceName).Run(); err != nil {
		return err
	}

	fmt.Println("✅ Linux Systemd 服务已安装并启动")
	return nil
}

func disableAutoStart() error {
	_ = exec.Command("systemctl", "stop", ServiceName).Run()
	if err := exec.Command("systemctl", "disable", ServiceName).Run(); err != nil {
		return fmt.Errorf("disable service failed: %v", err)
	}

	serviceFile := fmt.Sprintf("/etc/systemd/system/%s.service", ServiceName)
	os.Remove(serviceFile)
	exec.Command("systemctl", "daemon-reload").Run()

	fmt.Println("✅ Linux Systemd 服务已移除")
	return nil
}
