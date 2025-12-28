package executor

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"github.com/creack/pty"
)

type UnixPty struct {
	ptmx *os.File
}

func startTerminalPlatform(cmd *exec.Cmd) (TerminalSession, error) {
	// 使用 pty 库启动命令
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	return &UnixPty{ptmx: ptmx}, nil
}

func (p *UnixPty) Read(b []byte) (int, error) {
	return p.ptmx.Read(b)
}

func (p *UnixPty) Write(b []byte) (int, error) {
	return p.ptmx.Write(b)
}

func (p *UnixPty) Close() error {
	return p.ptmx.Close()
}

func (p *UnixPty) Resize(rows, cols int) error {
	sz := &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
		X:    0,
		Y:    0,
	}
	// 调用 Syscall 设置窗口大小
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		p.ptmx.Fd(),
		syscall.TIOCSWINSZ,
		uintptr(unsafe.Pointer(sz)),
	)
	if errno != 0 {
		return errno
	}
	return nil
}
