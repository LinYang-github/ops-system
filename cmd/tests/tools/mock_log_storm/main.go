package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"ops-system/pkg/protocol"

	"github.com/gorilla/websocket"
)

// ================= 配置参数 =================
var (
	masterURL  = flag.String("master", "http://127.0.0.1:8080", "Master HTTP 地址")
	wsURL      = flag.String("ws_master", "ws://127.0.0.1:8080", "Master WebSocket 地址")
	clients    = flag.Int("clients", 50, "并发日志查看客户端数量")
	duration   = flag.Duration("duration", 60*time.Second, "测试持续时间")
	workerPort = flag.Int("worker_port", 9999, "Mock Worker 监听端口")
	logRate    = flag.Int("rate", 1000, "单连接日志产生速率 (行/秒)")
	lineSize   = flag.Int("size", 512, "单行日志大小 (Bytes)")
	secret     = flag.String("secret", "ops-system-secret-key", "Auth Token")
	forceLocal = flag.Bool("local", true, "强制使用 127.0.0.1 (解决网络路由问题)")
)

// ================= 统计指标 =================
var (
	stats struct {
		TotalLines    int64
		TotalBytes    int64
		ActiveClients int64
		Errors        int64
	}
	myIP string
)

func main() {
	flag.Parse()
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	if *forceLocal {
		myIP = "127.0.0.1"
	} else {
		myIP = getLocalIP()
	}
	log.Printf("🔧 Mock Worker 将使用 IP: %s (Port: %d)", myIP, *workerPort)

	printBanner()

	// 1. 启动 Mock Worker
	serverReady := make(chan struct{})
	go startMockWorker(*workerPort, serverReady)
	<-serverReady

	// 2. 自动化注册流程
	log.Println("🔄 开始自动注册流程...")
	targetInstID, err := autoRegister()
	if err != nil {
		log.Printf("\n❌ 注册失败: %v", err)
		os.Exit(1)
	}
	log.Printf("✅ 自动化注册完成! 目标实例 ID: %s", targetInstID)

	// 3. 启动并发客户端
	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	log.Printf("🚀 启动日志风暴: %d 客户端, 持续 %v ...", *clients, *duration)
	var wg sync.WaitGroup

	go monitorStats(ctx)

	for i := 0; i < *clients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			runMockClient(ctx, id, targetInstID)
		}(i)
		time.Sleep(10 * time.Millisecond)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	select {
	case <-ctx.Done():
		log.Println("\n⏱️ 测试时间结束")
	case <-c:
		log.Println("\n🛑 手动停止")
		cancel()
	}

	wg.Wait()
	log.Println("✅ 测试完成")
}

// ================= Mock Worker =================

func startMockWorker(port int, readySig chan struct{}) {
	mux := http.NewServeMux()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	// 日志流接口
	mux.HandleFunc("/api/log/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		interval := time.Second / time.Duration(*logRate)
		if interval == 0 {
			interval = time.Microsecond
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		payload := make([]byte, *lineSize)
		for i := range payload {
			payload[i] = 'a' + byte(rand.Intn(26))
		}
		msg := string(payload)

		for {
			line := fmt.Sprintf("[%s] [INFO] %s", time.Now().Format("15:04:05.000"), msg)
			if err := conn.WriteMessage(websocket.TextMessage, []byte(line)); err != nil {
				return
			}
			time.Sleep(time.Millisecond * 2)
		}
	})

	// 纳管回调
	mux.HandleFunc("/api/external/register", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	addr := fmt.Sprintf("0.0.0.0:%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("端口被占用: %v", err)
	}

	close(readySig)
	if err := http.Serve(listener, mux); err != nil {
		log.Fatalf("Mock Worker 崩溃: %v", err)
	}
}

// ================= 注册逻辑 =================

func autoRegister() (string, error) {
	sysName := "LoadTestSystem"

	// 1. 尝试创建系统 (忽略错误，可能是已存在)
	_ = authPost("/api/systems/create", map[string]string{
		"name":        sysName,
		"description": "Log Storm Test",
	})

	// 2. 注册节点心跳
	hbData := protocol.RegisterRequest{
		Port: *workerPort,
		Info: protocol.NodeInfo{
			IP:       myIP,
			Hostname: "mock-worker-host",
			OS:       "linux",
			Arch:     "amd64",
			Status:   "online",
		},
		Status: protocol.NodeStatus{CPUUsage: 10, MemUsage: 20},
	}
	if err := authPost("/api/worker/heartbeat", hbData); err != nil {
		return "", fmt.Errorf("节点注册失败: %v", err)
	}

	// 3. 获取 System ID (修复点：使用 authGet)
	realSysID, err := getSystemID(sysName)
	if err != nil {
		return "", fmt.Errorf("获取 SystemID 失败: %v (Master 响应非预期)", err)
	}

	// 4. 纳管实例
	instName := "LogStormGenerator"
	reqData := struct {
		SystemID string                  `json:"system_id"`
		NodeIP   string                  `json:"node_ip"`
		Config   protocol.ExternalConfig `json:"config"`
	}{
		SystemID: realSysID,
		NodeIP:   myIP,
		Config:   protocol.ExternalConfig{Name: instName, WorkDir: "/tmp", StartCmd: "echo"},
	}

	// 发起纳管 (Master 会回调 MockWorker)
	if err := authPost("/api/deploy/external", reqData); err != nil {
		// 忽略纳管错误，因为可能已存在
		log.Printf("⚠️ 纳管请求返回: %v (尝试继续查找实例)", err)
	}

	// 5. 查找 Instance ID
	for i := 0; i < 5; i++ {
		time.Sleep(500 * time.Millisecond)
		instID, err := findInstanceID(realSysID, myIP)
		if err == nil && instID != "" {
			return instID, nil
		}
	}

	return "", fmt.Errorf("超时：未找到实例 ID")
}

// ================= 通信辅助函数 =================

// 【关键修复】带 Token 的 GET 请求
func authGet(path string) ([]byte, error) {
	req, _ := http.NewRequest("GET", *masterURL+path, nil)
	req.Header.Set("Authorization", "Bearer "+*secret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// 带 Token 的 POST 请求
func authPost(path string, data interface{}) error {
	jsonData, _ := json.Marshal(data)
	req, _ := http.NewRequest("POST", *masterURL+path, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+*secret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 读取 Body 用于错误诊断，但不强制要求 200 (交给调用方判断，或者这里统一判断)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func getSystemID(sysName string) (string, error) {
	// 使用 authGet 替代 http.Get
	body, err := authGet("/api/systems")
	if err != nil {
		return "", err
	}

	var res struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return "", err
	}

	for _, s := range res.Data {
		if s.Name == sysName {
			return s.ID, nil
		}
	}
	return "", fmt.Errorf("系统列表里没找到 %s", sysName)
}

func findInstanceID(sysID, nodeIP string) (string, error) {
	// 使用 authGet 替代 http.Get
	body, err := authGet("/api/systems")
	if err != nil {
		return "", err
	}

	var res struct {
		Data []struct {
			ID        string `json:"id"`
			Instances []struct {
				ID     string `json:"id"`
				NodeIP string `json:"node_ip"`
			} `json:"instances"`
		} `json:"data"`
	}
	json.Unmarshal(body, &res)

	for _, sys := range res.Data {
		if sys.ID == sysID {
			for _, inst := range sys.Instances {
				if inst.NodeIP == nodeIP {
					return inst.ID, nil
				}
			}
		}
	}
	return "", fmt.Errorf("not found")
}

// ================= Mock Client & Stats =================

func runMockClient(ctx context.Context, id int, instID string) {
	url := fmt.Sprintf("%s/api/instance/logs/stream?instance_id=%s&log_key=Console%%20Log", *wsURL, instID)
	header := http.Header{}
	header.Add("Authorization", "Bearer "+*secret)

	conn, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		atomic.AddInt64(&stats.Errors, 1)
		return
	}
	defer conn.Close()

	atomic.AddInt64(&stats.ActiveClients, 1)
	defer atomic.AddInt64(&stats.ActiveClients, -1)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			_, message, err := conn.ReadMessage()
			if err != nil {
				atomic.AddInt64(&stats.Errors, 1)
				return
			}
			atomic.AddInt64(&stats.TotalLines, 1)
			atomic.AddInt64(&stats.TotalBytes, int64(len(message)))
		}
	}
}

func monitorStats(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	var lastBytes int64 = 0

	fmt.Println("\n📊 日志风暴监控")
	fmt.Printf("%-10s | %-12s | %-12s | %-8s\n", "Clients", "Throughput", "Lines/s", "Errors")
	fmt.Println("-------------------------------------------------------")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			currBytes := atomic.LoadInt64(&stats.TotalBytes)
			currLines := atomic.LoadInt64(&stats.TotalLines)
			active := atomic.LoadInt64(&stats.ActiveClients)
			errs := atomic.LoadInt64(&stats.Errors)

			bw := float64(currBytes-lastBytes) / 1024 / 1024
			fmt.Printf("\r%-10d | %-10.2f MB/s | %-12d | %-8d", active, bw, currLines, errs)

			lastBytes = currBytes
			atomic.StoreInt64(&stats.TotalLines, 0)
		}
	}
}

func getLocalIP() string { return "127.0.0.1" } // 简化，直接返回

func printBanner() {
	fmt.Println(`
  _      ____   _____   _____ _______ ____  _____  __  __ 
 | |    / __ \ / ____| / ____|__   __/ __ \|  __ \|  \/  |
 | |   | |  | | |  __ | (___    | | | |  | | |__) | \  / |
 | |   | |  | | | |_ | \___ \   | | | |  | |  _  /| |\/| |
 | |___| |__| | |__| | ____) |  | | | |__| | | \ \| |  | |
 |______\____/ \_____||_____/   |_|  \____/|_|  \_\_|  |_|
`)
}
