package executor

import (
	"fmt"
	"os/exec"
	"syscall"
)

// prepareProcess 设置 Session ID，使进程成为新进程组的 Leader
// 这一步至关重要，没有它，killProcessTree 里的 -pid 就无法生效
func prepareProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
}

// attach
func attachProcessToManager(uniqueID string, pid int) error {
	return nil
}

// killProcessTree 杀掉进程组
func killProcessTree(pid int, uniqueID string) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid: %d", pid)
	}

	// 1. 尝试杀掉进程组 (负数 PID)
	// SIGKILL (9) 强制杀死，不可被捕获
	err := syscall.Kill(-pid, syscall.SIGKILL)

	if err == nil {
		return nil // 成功发送信号
	}

	// 2. 错误处理
	// 如果错误是 ESRCH (no such process)，说明进程组已经不存在了，视为成功
	if err == syscall.ESRCH {
		return nil
	}

	// 3. 降级：如果杀进程组失败 (比如权限问题，或者并未成功建立进程组)
	// 尝试杀单个进程
	errSingle := syscall.Kill(pid, syscall.SIGKILL)
	if errSingle == nil || errSingle == syscall.ESRCH {
		return nil
	}

	// 返回最初的错误，或者汇总错误
	return fmt.Errorf("kill group failed: %v, kill single failed: %v", err, errSingle)
}
