package executor

import (
	"os/exec"
	"syscall"
)

// setProcessAttributes 设置进程属性 (Linux/Mac)
// 使用 Setsid 创建新的会话，彻底脱离父进程和终端
func setProcessAttributes(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
}
