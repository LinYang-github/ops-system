package utils

import (
	"fmt"
	"os"
	"os/exec"
)

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.ops.worker</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
%s
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/ops-worker.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/ops-worker.err</string>
</dict>
</plist>
`

func enableAutoStart(exePath string, args []string) error {
	plistPath := "/Library/LaunchDaemons/com.ops.worker.plist"

	// 构造参数 XML 部分
	argsXml := ""
	for _, arg := range args {
		argsXml += fmt.Sprintf("        <string>%s</string>\n", arg)
	}

	content := fmt.Sprintf(plistTemplate, exePath, argsXml)

	if err := os.WriteFile(plistPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write plist failed (sudo?): %v", err)
	}

	// 加载服务
	exec.Command("launchctl", "unload", plistPath).Run() // 先卸载旧的
	if err := exec.Command("launchctl", "load", plistPath).Run(); err != nil {
		return err
	}

	fmt.Println("✅ macOS Launchd 服务已加载")
	return nil
}

func disableAutoStart() error {
	plistPath := "/Library/LaunchDaemons/com.ops.worker.plist"

	exec.Command("launchctl", "unload", plistPath).Run()
	os.Remove(plistPath)

	fmt.Println("✅ macOS Launchd 服务已移除")
	return nil
}
