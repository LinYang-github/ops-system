package ws

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WsMessage 推送给前端的消息结构
type WsMessage struct {
	Type string      `json:"type"` // "nodes" or "systems"
	Data interface{} `json:"data"`
}

type Hub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan WsMessage
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.Mutex

	// 数据缓存 (用于节流)
	latestNodes   interface{}
	latestSystems interface{}
	nodesDirty    bool
	systemsDirty  bool
}

var (
	GlobalHub = NewHub()
	upgrader  = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true }, // 允许跨域
	}
)

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan WsMessage),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
	}
}

func (h *Hub) Run() {
	// 启动一个节流推送协程：每 1 秒检查一次是否有数据更新
	go h.throttlePush()

	for {
		select {
		case conn := <-h.register:
			h.mu.Lock()
			h.clients[conn] = true
			h.mu.Unlock()
			// 连接建立时，立即发送一次当前状态
			h.pushCurrentState(conn)

		case conn := <-h.unregister:
			h.disconnect(conn)

		case msg := <-h.broadcast:
			// 收到更新请求，只更新缓存并标记为 Dirty，不立即发送
			h.mu.Lock()
			if msg.Type == "nodes" {
				h.latestNodes = msg.Data
				h.nodesDirty = true
			} else if msg.Type == "systems" {
				h.latestSystems = msg.Data
				h.systemsDirty = true
			}
			h.mu.Unlock()
		}
	}
}

// 节流推送逻辑
func (h *Hub) throttlePush() {
	ticker := time.NewTicker(1 * time.Second) // 1秒推送一次
	defer ticker.Stop()

	for range ticker.C {
		h.mu.Lock()
		var msgs []WsMessage

		if h.nodesDirty && h.latestNodes != nil {
			msgs = append(msgs, WsMessage{Type: "nodes", Data: h.latestNodes})
			h.nodesDirty = false
		}
		if h.systemsDirty && h.latestSystems != nil {
			msgs = append(msgs, WsMessage{Type: "systems", Data: h.latestSystems})
			h.systemsDirty = false
		}
		h.mu.Unlock()

		if len(msgs) > 0 {
			h.broadcastToAll(msgs)
		}
	}
}

func (h *Hub) broadcastToAll(msgs []WsMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for conn := range h.clients {
		for _, msg := range msgs {
			if err := conn.WriteJSON(msg); err != nil {
				log.Printf("WS write error: %v", err)
				conn.Close()
				delete(h.clients, conn)
				break
			}
		}
	}
}

func (h *Hub) disconnect(conn *websocket.Conn) {
	h.mu.Lock()
	if _, ok := h.clients[conn]; ok {
		delete(h.clients, conn)
		conn.Close()
	}
	h.mu.Unlock()
}

// 新连接建立时，推送现有缓存的数据
func (h *Hub) pushCurrentState(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.latestNodes != nil {
		conn.WriteJSON(WsMessage{Type: "nodes", Data: h.latestNodes})
	}
	if h.latestSystems != nil {
		conn.WriteJSON(WsMessage{Type: "systems", Data: h.latestSystems})
	}
}

// Public Methods to trigger update
func BroadcastNodes(data interface{}) {
	GlobalHub.broadcast <- WsMessage{Type: "nodes", Data: data}
}

func BroadcastSystems(data interface{}) {
	GlobalHub.broadcast <- WsMessage{Type: "systems", Data: data}
}

// HandleWebsocket 供 Server 路由调用
func HandleWebsocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}
	GlobalHub.register <- conn

	// 保持连接读取，处理 Close 消息
	go func() {
		defer func() { GlobalHub.unregister <- conn }()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}()
}
