package handler

import (
	"encoding/json"
	"log"
	"net/http"
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

// HandleTerminal 处理终端 WebSocket
func HandleTerminal(w http.ResponseWriter, r *http.Request) {
	// 1. 升级 WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// 2. 准备 Shell 命令
	var shell string
	var args []string
	if runtime.GOOS == "windows" {
		shell = "cmd.exe"
	} else {
		shell = "/bin/bash"
		// 尝试使用 login shell
		args = []string{"-l"}
	}

	cmd := exec.Command(shell, args...)
	// 设置环境变量，模拟终端类型
	cmd.Env = append(cmd.Env, "TERM=xterm-256color")

	// 3. 启动 PTY
	tty, err := executor.StartTerminal(cmd)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Error starting shell: "+err.Error()))
		return
	}
	defer tty.Close()

	// 4. 管道处理
	errChan := make(chan error, 2)

	// PTY -> WebSocket (输出)
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := tty.Read(buf)
			if err != nil {
				errChan <- err
				return
			}
			// 发送二进制数据
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				errChan <- err
				return
			}
		}
	}()

	// WebSocket -> PTY (输入 & 控制)
	go func() {
		for {
			mt, message, err := conn.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}

			if mt == websocket.BinaryMessage {
				// 二进制消息直接当作输入
				tty.Write(message)
			} else if mt == websocket.TextMessage {
				// 文本消息解析为控制指令
				var msg TerminalMessage
				if err := json.Unmarshal(message, &msg); err == nil {
					if msg.Type == "resize" {
						tty.Resize(msg.Rows, msg.Cols)
					}
				}
			}
		}
	}()

	// 等待退出
	<-errChan
	log.Println("Terminal session closed")
}
