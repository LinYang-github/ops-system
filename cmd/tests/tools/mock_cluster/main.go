package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"ops-system/pkg/protocol"

	"github.com/gorilla/websocket"
)

// ==========================================
// 配置参数
// ==========================================
var (
	masterURL  = flag.String("master", "http://127.0.0.1:8080", "Master 服务地址 (http://...)")
	nodeCount  = flag.Int("count", 200, "模拟节点数量")
	duration   = flag.Duration("duration", 5*time.Minute, "测试持续时间")
	interval   = flag.Duration("interval", 5*time.Second, "心跳间隔")
	packetLoss = flag.Int("loss", 0, "模拟丢包率 (0-100%) - 仅跳过发送，不断连")
	secret     = flag.String("secret", "ops-system-secret-key", "认证 Token")
	startRate  = flag.Int("rate", 50, "启动速率 (每秒启动多少个连接)")
)

// ==========================================
// 统计指标
// ==========================================
var (
	stats struct {
		SentBytes   int64 // 发送字节数
		RecvBytes   int64 // 接收字节数 (Master 下发的配置/指令)
		SentCount   int64 // 发送消息数 (心跳)
		ConnectFail int64 // 连接失败数
		Disconnect  int64 // 断开连接数
		ActiveConns int64 // 当前在线连接数
	}
	startTime = time.Now()
)

func main() {
	flag.Parse()
	printBanner()

	// 1. URL 转换 http -> ws
	wsURL := convertToWS(*masterURL)
	log.Printf("🎯 目标 Master WS: %s", wsURL)

	// 2. 信号处理
	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 3. 启动监控面板
	go monitorStats(ctx)

	// 4. 启动模拟节点 (流控启动)
	var wg sync.WaitGroup
	ticker := time.NewTicker(time.Second / time.Duration(*startRate))

	log.Printf("🚀 正在启动 %d 个模拟节点 (速率: %d/s)...", *nodeCount, *startRate)

	for i := 0; i < *nodeCount; i++ {
		select {
		case <-ctx.Done():
			break
		case <-sigChan:
			cancel()
			goto CLEANUP
		case <-ticker.C:
			wg.Add(1)
			// 生成确定性 IP
			mockIP := fmt.Sprintf("10.%d.%d.%d", (i/65536)%255, (i/256)%255, i%255+1)
			mockName := fmt.Sprintf("stress-worker-%04d", i)

			go func(ip, name string, idx int) {
				defer wg.Done()
				runMockWebSocketWorker(ctx, wsURL, ip, name, idx)
			}(mockIP, mockName, i)
		}
	}
	ticker.Stop()

	// 5. 等待结束
	select {
	case <-ctx.Done():
		log.Println("\n⏱️ 测试时间结束")
	case <-sigChan:
		log.Println("\n🛑 接收到停止信号")
		cancel()
	}

CLEANUP:
	log.Println("正在等待所有连接关闭...")
	wg.Wait()
	printFinalReport()
}

// ==========================================
// 模拟 Worker (WebSocket 版本)
// ==========================================

func runMockWebSocketWorker(ctx context.Context, wsURL, ip, name string, offset int) {
	// 1. 建立连接
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+*secret)

	// 为了模拟真实网络，每个连接使用新的 Dialer
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 5 * time.Second

	conn, _, err := dialer.Dial(wsURL, headers)
	if err != nil {
		atomic.AddInt64(&stats.ConnectFail, 1)
		// 连接失败直接退出，或者可以实现重连逻辑 (这里简单处理为退出)
		return
	}
	defer conn.Close()

	atomic.AddInt64(&stats.ActiveConns, 1)
	defer atomic.AddInt64(&stats.ActiveConns, -1)
	defer atomic.AddInt64(&stats.Disconnect, 1)

	// 2. 发送注册包 (TypeRegister)
	if err := sendPacket(conn, protocol.TypeRegister, ip, name, offset); err != nil {
		return
	}

	// 3. 启动读协程 (必须读取，否则缓冲区满会导致断开，同时也为了处理 Master 的 Ping)
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			atomic.AddInt64(&stats.RecvBytes, int64(len(msg)))
		}
	}()

	// 4. 心跳循环
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// 发送关闭帧
			conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return
		case <-ticker.C:
			// 模拟丢包
			if *packetLoss > 0 && rand.Intn(100) < *packetLoss {
				continue
			}

			// 发送心跳包
			if err := sendPacket(conn, protocol.TypeHeartbeat, ip, name, offset); err != nil {
				return // 发送失败视为断开
			}
		}
	}
}

// 构造并发送数据包
func sendPacket(conn *websocket.Conn, msgType, ip, name string, offset int) error {
	// 模拟负载波动
	now := float64(time.Now().Unix())
	phaseShift := float64(offset) * 0.1
	cpuLoad := 20.0 + 30.0*math.Sin(now/60.0+phaseShift) + rand.Float64()*10
	memLoad := 40.0 + 20.0*math.Sin(now/300.0+phaseShift) + rand.Float64()*5

	// 构造 Paylaod
	info := protocol.NodeInfo{
		ID:        fmt.Sprintf("node-id-%s", name), // 确定的 NodeID
		IP:        ip,
		Port:      8081,
		Hostname:  name,
		Name:      name,
		OS:        "linux",
		Arch:      "amd64",
		CPUCores:  8,
		MemTotal:  32768,
		DiskTotal: 1024 * 1024 * 1024 * 500,
		Status:    "online",
	}

	status := protocol.NodeStatus{
		CPUUsage:    cpuLoad,
		MemUsage:    memLoad,
		DiskUsage:   50.0,
		NetInSpeed:  rand.Float64() * 1000,
		NetOutSpeed: rand.Float64() * 2000,
		Uptime:      uint64(time.Since(startTime).Seconds()),
		Time:        time.Now().Unix(),
	}

	req := protocol.RegisterRequest{
		Port:   8081,
		Info:   info,
		Status: status,
	}

	// 封装 WS 协议
	wsMsg, _ := protocol.NewWSMessage(msgType, "", req)

	// 写入
	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	err := conn.WriteJSON(wsMsg)
	if err == nil {
		atomic.AddInt64(&stats.SentCount, 1)
		atomic.AddInt64(&stats.SentBytes, int64(len(wsMsg.Payload))) // 近似值
	}
	return err
}

// ==========================================
// 辅助函数
// ==========================================

func convertToWS(rawURL string) string {
	u, _ := url.Parse(rawURL)
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	// 注意路径必须匹配 Master 路由
	u.Path = "/api/worker/ws"
	return u.String()
}

func monitorStats(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastSent int64 = 0

	fmt.Println("\n📊 实时监控 (WebSocket Mode)")
	fmt.Printf("%-8s | %-8s | %-10s | %-10s | %-8s\n", "Active", "ConFail", "Msgs/s", "MB Sent", "MB Recv")
	fmt.Println("---------------------------------------------------------------")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			active := atomic.LoadInt64(&stats.ActiveConns)
			fail := atomic.LoadInt64(&stats.ConnectFail)
			currSent := atomic.LoadInt64(&stats.SentCount)
			bytesSent := atomic.LoadInt64(&stats.SentBytes)
			bytesRecv := atomic.LoadInt64(&stats.RecvBytes)

			qps := currSent - lastSent
			mbSent := float64(bytesSent) / 1024 / 1024
			mbRecv := float64(bytesRecv) / 1024 / 1024

			fmt.Printf("\r%-8d | %-8d | %-10d | %-10.2f | %-8.2f",
				active, fail, qps, mbSent, mbRecv)

			lastSent = currSent
		}
	}
}

func printBanner() {
	fmt.Println(`
   __  __  ___   ___ _  __   ___ _    _   _ ___ _____ ___ ___ 
  |  \/  |/ _ \ / __| |/ /  / __| |  | | | / __|_   _| __| _ \
  | |\/| | (_) | (__| ' <  | (__| |__| |_| \__ \ | | | _||   /
  |_|  |_|\___/ \___|_|\_\  \___|____|\___/|___/ |_| |___|_|_\
                                                              
  >> GDOS Mock Cluster (WebSocket Edition)
	`)
}

func printFinalReport() {
	fmt.Println("\n\n📋 测试报告")
	fmt.Println("========================================")
	fmt.Printf("总发送消息:  %d\n", atomic.LoadInt64(&stats.SentCount))
	fmt.Printf("连接失败数:  %d\n", atomic.LoadInt64(&stats.ConnectFail))
	fmt.Printf("异常断开数:  %d\n", atomic.LoadInt64(&stats.Disconnect))
	fmt.Println("========================================")
}
