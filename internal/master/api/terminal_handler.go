package api

import (
	"fmt"
	"net/http"

	"github.com/gorilla/websocket"
)

// HandleNodeTerminal 代理节点终端
func (h *ServerHandler) HandleNodeTerminal(w http.ResponseWriter, r *http.Request) {
	nodeIP := r.URL.Query().Get("ip")

	node, exists := h.nodeMgr.GetNode(nodeIP)
	if !exists {
		http.Error(w, "Node offline", 404)
		return
	}

	// 构造 Worker URL
	targetURL := fmt.Sprintf("ws://%s:%d/api/terminal/ws", node.IP, node.Port)

	// 1. 连接 Worker
	workerConn, _, err := websocket.DefaultDialer.Dial(targetURL, nil)
	if err != nil {
		http.Error(w, "Connect worker failed: "+err.Error(), 502)
		return
	}
	defer workerConn.Close()

	// 2. 升级前端
	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer clientConn.Close()

	// 3. 双向转发 (透传)
	errChan := make(chan error, 2)

	go func() {
		for {
			mt, msg, err := workerConn.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}
			if err := clientConn.WriteMessage(mt, msg); err != nil {
				errChan <- err
				return
			}
		}
	}()

	go func() {
		for {
			mt, msg, err := clientConn.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}
			if err := workerConn.WriteMessage(mt, msg); err != nil {
				errChan <- err
				return
			}
		}
	}()

	<-errChan
}
