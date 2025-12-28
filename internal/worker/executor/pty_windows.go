package executor

import (
	"io"
	"os/exec"
	"sync"
)

// WindowsPipe 模拟终端
type WindowsPipe struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	mu     sync.Mutex
	closed bool
}

func startTerminalPlatform(cmd *exec.Cmd) (TerminalSession, error) {
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &WindowsPipe{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}, nil
}

func (w *WindowsPipe) Read(b []byte) (int, error) {
	// 简单地只读 stdout，实际可能需要合并 stderr
	return w.stdout.Read(b)
}

func (w *WindowsPipe) Write(b []byte) (int, error) {
	return w.stdin.Write(b)
}

func (w *WindowsPipe) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	w.cmd.Process.Kill()
	return nil
}

func (w *WindowsPipe) Resize(rows, cols int) error {
	// 管道模式不支持 Resize，忽略
	return nil
}
