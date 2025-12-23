package executor

import (
	"os/exec"
	"syscall"
)

// setProcessAttributes 设置进程属性 (Windows)
// 使用 CREATE_NEW_PROCESS_GROUP 标志，使子进程脱离父进程的控制台组
func setProcessAttributes(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}
