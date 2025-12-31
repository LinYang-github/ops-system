package transport

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"ops-system/internal/worker/agent"
	"ops-system/internal/worker/executor"
	"ops-system/pkg/protocol"

	"github.com/gorilla/websocket"
)

var GlobalClient *WorkerClient

type WorkerClient struct {
	MasterURL        string
	Secret           string
	Conn             *websocket.Conn
	SendChan         chan *protocol.WSMessage
	updateTickerChan chan time.Duration
}

func StartClient(masterURL, secret string) {
	client := &WorkerClient{
		MasterURL:        masterURL,
		Secret:           secret,
		SendChan:         make(chan *protocol.WSMessage, 64),
		updateTickerChan: make(chan time.Duration, 1), // 缓冲1
	}
	GlobalClient = client // [新增] 赋值给全局变量
	go client.connectLoop()
}

func (c *WorkerClient) connectLoop() {
	for {
		// 1. 构造 WebSocket URL
		// 使用 net/url 进行规范化处理，避免字符串拼接错误
		u, err := url.Parse(c.MasterURL)
		if err != nil {
			log.Printf("❌ Fatal: Invalid Master URL config: %v", err)
			return // 配置错误，直接退出或等待
		}

		// 修正 Scheme (http -> ws, https -> wss)
		switch u.Scheme {
		case "https":
			u.Scheme = "wss"
		case "http":
			u.Scheme = "ws"
		default:
			// 如果没写 scheme (如 "127.0.0.1:8080")，默认走 ws
			u.Scheme = "ws"
		}

		// 安全拼接路径
		u.Path = "/api/worker/ws"
		wsURL := u.String()

		header := http.Header{}
		header.Set("Authorization", "Bearer "+c.Secret)

		// 增加拨号超时
		dialer := websocket.DefaultDialer
		dialer.HandshakeTimeout = 5 * time.Second

		conn, _, err := dialer.Dial(wsURL, header)
		if err != nil {
			log.Printf("⚠️ Connect failed: %v. Retry in 5s...", err)
			time.Sleep(5 * time.Second)
			continue
		}

		c.Conn = conn
		log.Printf("✅ WebSocket Connected!")

		// 连接成功后，立即发送一次心跳作为注册包
		c.sendHeartbeat()

		// 启动子协程
		// 注意：如果不使用 Context 控制退出，断线重连时旧的协程可能会泄露
		// 但在这个简单模型中，writePump 会因为 Write 错误退出，heartbeatLoop 依赖外部断开
		// 为了健壮性，我们可以引入 stopChan，但在 MVP 中先保持简单
		go c.heartbeatLoop()
		go c.writePump()

		// 3. 阻塞读取 (主循环)
		c.readLoop()

		// 4. 断开清理
		c.Conn = nil
		log.Printf("❌ Disconnected. Reconnecting...")
		time.Sleep(2 * time.Second)
	}
}

func (c *WorkerClient) readLoop() {
	for {
		if c.Conn == nil {
			return
		}

		_, bytes, err := c.Conn.ReadMessage()
		if err != nil {
			log.Printf("Read error: %v", err)
			return
		}

		var msg protocol.WSMessage
		if err := json.Unmarshal(bytes, &msg); err != nil {
			log.Printf("JSON parse error: %v", err)
			continue
		}

		// 分发处理
		switch msg.Type {
		case protocol.TypeCommand:
			c.handleCommand(msg)
		case protocol.TypeConfig: // [新增] 处理配置下发
			c.handleConfig(msg)
		}
	}
}

// [修改] 支持动态调整的心跳循环
func (c *WorkerClient) heartbeatLoop() {
	// 默认 5s
	interval := 5 * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if c.Conn == nil {
				return
			}
			c.sendHeartbeat()

		case newInterval := <-c.updateTickerChan:
			// 如果配置变了，重置 Ticker
			if newInterval != interval && newInterval > 0 {
				log.Printf("🔄 Updating heartbeat interval: %v -> %v", interval, newInterval)
				interval = newInterval
				ticker.Reset(interval)
			}
		}
	}
}

func (c *WorkerClient) sendHeartbeat() {
	info := agent.GetNodeInfo()
	status := agent.GetStatus()
	req := protocol.RegisterRequest{Info: info, Status: status}

	wsMsg, _ := protocol.NewWSMessage(protocol.TypeRegister, "", req)

	// 非阻塞发送
	select {
	case c.SendChan <- wsMsg:
	default:
		// 缓冲区满，可能是断网了，忽略
	}
}

func (c *WorkerClient) writePump() {
	for msg := range c.SendChan {
		if c.Conn == nil {
			break
		}

		c.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := c.Conn.WriteJSON(msg); err != nil {
			log.Printf("WS Write Error: %v", err)
			c.Conn.Close()
			return
		}
	}
}

func (c *WorkerClient) handleCommand(msg protocol.WSMessage) {
	// 增加调试日志，方便观察指令是否到达
	log.Printf("📥 Received WS Message Type: %s", msg.Type)

	// 1. 通用 Map 解析 (为了灵活性)
	var rawMap map[string]string
	if err := json.Unmarshal(msg.Payload, &rawMap); err == nil {
		if rawMap["action"] == "start_terminal" {
			go c.startReverseTerminal(rawMap["server"], rawMap["session_id"])
			return
		}
	}

	// 1. 处理 InstanceActionRequest (启动/停止)
	var actionReq protocol.InstanceActionRequest
	if err := json.Unmarshal(msg.Payload, &actionReq); err == nil && actionReq.Action != "" {
		log.Printf("执行实例操作: %s -> %s", actionReq.InstanceID, actionReq.Action)
		if err := executor.HandleAction(actionReq); err != nil {
			log.Printf("操作失败: %v", err)
		}
		return
	}

	// 2. 处理 DeployRequest (部署)
	var deployReq protocol.DeployRequest
	if err := json.Unmarshal(msg.Payload, &deployReq); err == nil && deployReq.DownloadURL != "" {
		log.Printf("开始异步部署: %s", deployReq.ServiceName)
		// 必须异步，否则会阻塞心跳
		go func() {
			executor.ReportStatus(deployReq.InstanceID, "deploying", 0, 0)
			if err := executor.DeployInstance(deployReq); err != nil {
				executor.ReportStatus(deployReq.InstanceID, "error", 0, 0)
			} else {
				executor.ReportStatus(deployReq.InstanceID, "stopped", 0, 0)
			}
		}()
		return
	}

	// 3. 尝试 CommandRequest (CMD)
	var cmdReq protocol.CommandRequest
	if err := json.Unmarshal(msg.Payload, &cmdReq); err == nil && cmdReq.Command != "" {
		log.Printf("📥 Received CMD (Ignored in MVP): %s", cmdReq.Command)
	}
}

// [新增] 处理配置消息
func (c *WorkerClient) handleConfig(msg protocol.WSMessage) {
	var cfg protocol.HeartbeatResponse
	if err := json.Unmarshal(msg.Payload, &cfg); err != nil {
		return
	}

	// 1. 更新心跳间隔 (发送给 heartbeatLoop)
	if cfg.HeartbeatInterval > 0 {
		c.updateTickerChan <- time.Duration(cfg.HeartbeatInterval) * time.Second
	}

	// 2. 更新本地监控间隔 (直接调用 executor)
	if cfg.MonitorInterval > 0 {
		executor.UpdateMonitorInterval(cfg.MonitorInterval)
	}
}

// [新增] 启动反向终端
func (c *WorkerClient) startReverseTerminal(serverHost, sessionID string) {
	// 构造 Relay URL
	// 注意：这里需要处理 ws/wss，简单起见假设和 MasterURL 同协议
	scheme := "ws"
	if strings.HasPrefix(c.MasterURL, "https") {
		scheme = "wss"
	}

	relayURL := fmt.Sprintf("%s://%s/api/worker/terminal/relay?session_id=%s", scheme, serverHost, sessionID)
	log.Printf("Terminal: Connecting to relay %s", relayURL)

	// 【关键修复】添加鉴权头
	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.Secret)

	conn, _, err := websocket.DefaultDialer.Dial(relayURL, nil)
	if err != nil {
		log.Printf("Terminal Relay Dial failed: %v", err)
		return
	}
	defer conn.Close()

	// 启动 PTY
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

	// 使用 executor 中的工具启动 PTY
	tty, err := executor.StartTerminal(cmd)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Error starting shell: "+err.Error()))
		return
	}
	defer tty.Close()

	// 管道转发
	errChan := make(chan error, 2)

	// PTY -> WebSocket
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

	// WebSocket -> PTY
	type TermMsg struct {
		Type string `json:"type"`
		Rows int    `json:"rows"`
		Cols int    `json:"cols"`
		Data string `json:"data"`
	}

	go func() {
		for {
			mt, message, err := conn.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}

			if mt == websocket.BinaryMessage {
				tty.Write(message)
			} else if mt == websocket.TextMessage {
				var msg TermMsg
				if err := json.Unmarshal(message, &msg); err == nil {
					if msg.Type == "resize" {
						tty.Resize(msg.Rows, msg.Cols)
					}
				}
			}
		}
	}()

	<-errChan
	log.Println("Terminal session ended")
}

// [新增] 供外部模块调用的发送方法
func (c *WorkerClient) SendStatusReport(report protocol.InstanceStatusReport) {
	if c == nil || c.Conn == nil {
		return
	}
	// 封装为 WSMessage
	msg, _ := protocol.NewWSMessage(protocol.TypeStatusReport, "", report)

	// 非阻塞发送
	select {
	case c.SendChan <- msg:
	default:
		// 缓冲区满则丢弃，监控数据允许少量丢失
	}
}
