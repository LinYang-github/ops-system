package handler

import (
	"encoding/json"
	"log"
	"os/exec"
	"runtime"

	"ops-system/internal/worker/executor"

	"github.com/gorilla/websocket"
)

type TerminalMessage struct {
	Type string `json:"type"` // "resize" | "input"
	Rows int    `json:"rows"`
	Cols int    `json:"cols"`
	Data string `json:"data"` // Base64 or plain text if needed
}

// ServeTerminal 直接处理 WebSocket 连接 (供 Tunnel 调用)
// 终端逻辑目前不依赖 executor 的状态 (只依赖无状态的 StartTerminal)，所以暂时不需要传 Manager
func ServeTerminal(conn *websocket.Conn) {
	defer conn.Close() // 确保退出时关闭

	// 1. 准备 Shell
	var shell string
	var args []string
	if runtime.GOOS == "windows" {
		shell = "cmd.exe"
	} else {
		shell = "/bin/bash"
		args = []string{"-l"}
	}

	cmd := exec.Command(shell, args...)
	cmd.Env = append(cmd.Env, "TERM=xterm-256color")

	// 2. 启动 PTY (executor.StartTerminal 是无状态函数)
	tty, err := executor.StartTerminal(cmd)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Error starting shell: "+err.Error()))
		return
	}
	defer tty.Close()

	// 3. 管道转发
	errChan := make(chan error, 2)

	// PTY -> WS
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := tty.Read(buf)
			if err != nil {
				errChan <- err
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				errChan <- err
				return
			}
		}
	}()

	// WS -> PTY
	go func() {
		for {
			mt, message, err := conn.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}
			// 处理输入
			if mt == websocket.BinaryMessage {
				tty.Write(message)
			} else if mt == websocket.TextMessage {
				// 处理 Resize 指令
				var msg struct {
					Type string `json:"type"`
					Rows int    `json:"rows"`
					Cols int    `json:"cols"`
				}
				if err := json.Unmarshal(message, &msg); err == nil && msg.Type == "resize" {
					tty.Resize(msg.Rows, msg.Cols)
				}
			}
		}
	}()

	<-errChan
	log.Println("Terminal session closed")
}
