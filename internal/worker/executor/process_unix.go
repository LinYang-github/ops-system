package executor

import (
	"fmt"
	"os/exec"
	"syscall"
)

// prepareProcess 设置 Session ID，使进程成为新进程组的 Leader
func prepareProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // 关键：创建新会话
	}
}

// attachProcessToManager Linux 下不需要额外的 Attach
func attachProcessToManager(uniqueID string, pid int) error {
	return nil
}

// killProcessTree 杀掉进程组
func killProcessTree(pid int, uniqueID string) error {
	// 使用负数 PID 发送信号给进程组
	// 注意：必须是 -pid
	if pid <= 0 {
		return fmt.Errorf("invalid pid")
	}

	// 发送 SIGKILL 给进程组
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if err != nil {
		// 如果进程组不存在，尝试杀单个进程 (保底)
		return syscall.Kill(pid, syscall.SIGKILL)
	}
	return nil
}
