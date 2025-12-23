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

// handleLogStream 处理日志 WebSocket 连接
// URL: /api/log/ws?instance_id=...&log_key=...
func handleLogStream(w http.ResponseWriter, r *http.Request) {
	// 1. 升级 WS
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WS upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	// 2. 获取参数
	instID := r.URL.Query().Get("instance_id")
	logKey := r.URL.Query().Get("log_key") // e.g. "Console Log"

	// 3. 获取物理路径
	logPath, err := executor.GetLogPath(instID, logKey)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Error: "+err.Error()))
		return
	}

	// 4. 检查文件是否存在
	// tail 库支持 ReOpen，但为了用户体验，如果文件完全不存在，先发个提示
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Waiting for log file to be created: %s...\n", logPath)))
	}

	// 5. 开始 Tail
	// Config: Follow=true (持续监听), ReOpen=true (支持日志轮转/文件重建), MustExist=false (容忍文件暂不存在)
	t, err := tail.TailFile(logPath, tail.Config{
		Follow:    true,
		ReOpen:    true,
		MustExist: false,
		Poll:      true, // Windows下建议开启 Poll 模式以获得更好兼容性
	})
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Tail Error: "+err.Error()))
		return
	}

	// 清理函数
	defer t.Stop()

	// 6. 循环推送
	// 启动一个协程监听客户端关闭，以便退出 tail 循环
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				t.Stop() // 客户端断开，停止 tail
				return
			}
		}
	}()

	for line := range t.Lines {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(line.Text)); err != nil {
			break // 发送失败（客户端断开），退出
		}
	}
}
