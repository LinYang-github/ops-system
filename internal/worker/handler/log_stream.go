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

// HandleLogStream HTTP 包装函数 (供 server.go 路由调用)
// 挂载到 WorkerHandler 以访问 execMgr
func (h *WorkerHandler) HandleLogStream(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Log stream upgrade failed: %v", err)
		return
	}
	// 解析 Query 参数
	instID := r.URL.Query().Get("instance_id")
	logKey := r.URL.Query().Get("log_key")

	// 传入 h.execMgr
	ServeLogStream(conn, instID, logKey, h.execMgr)
}

// ServeLogStream 直接处理 WebSocket 连接 (供 Tunnel 和 Handler 调用)
// 增加 execMgr 参数，因为 GetLogPath 现在是方法
func ServeLogStream(conn *websocket.Conn, instID, logKey string, execMgr *executor.Manager) {
	defer conn.Close()

	// 1. 获取路径 (使用 execMgr 实例)
	logPath, err := execMgr.GetLogPath(instID, logKey)
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
