package handler

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"ops-system/internal/worker/executor"

	"github.com/gorilla/websocket"
	"github.com/hpcloud/tail"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// [新增] HTTP 包装函数 (供 server.go 调用)
func handleLogStream(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Log stream upgrade failed: %v", err)
		return
	}
	// 解析 Query 参数
	instID := r.URL.Query().Get("instance_id")
	logKey := r.URL.Query().Get("log_key")

	ServeLogStream(conn, instID, logKey)
}

// ServeLogStream 直接处理 WebSocket 连接 (供 Tunnel 调用)
func ServeLogStream(conn *websocket.Conn, instID, logKey string) {
	defer conn.Close()

	// 1. 获取路径
	logPath, err := executor.GetLogPath(instID, logKey)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Error: "+err.Error()))
		return
	}

	// 2. 检查文件
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Waiting for log file: %s...\n", logPath)))
	}

	// 3. Tail
	t, err := tail.TailFile(logPath, tail.Config{
		Follow: true, ReOpen: true, MustExist: false, Poll: true,
	})
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Tail Error: "+err.Error()))
		return
	}
	defer t.Stop()

	// 4. 监听断开 (保持连接活跃检测)
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				t.Stop()
				return
			}
		}
	}()

	// 5. 推送日志
	for line := range t.Lines {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(line.Text)); err != nil {
			break
		}
	}
}
