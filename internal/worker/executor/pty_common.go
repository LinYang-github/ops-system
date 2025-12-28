package executor

import (
	"io"
	"os/exec"
)

// TerminalSession 定义终端会话的操作接口
type TerminalSession interface {
	io.Reader
	io.Writer
	io.Closer
	// Resize 调整窗口大小 (行, 列)
	Resize(rows, cols int) error
}

// StartTerminal 启动一个终端进程
// cmd: 要启动的命令 (如 /bin/bash 或 cmd.exe)
// 返回: 终端会话接口
func StartTerminal(cmd *exec.Cmd) (TerminalSession, error) {
	return startTerminalPlatform(cmd)
}
