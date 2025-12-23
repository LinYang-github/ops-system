package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"ops-system/pkg/protocol"
)

// 配置
var (
	masterURL   = flag.String("master", "http://127.0.0.1:8080", "Master Address")
	workerCount = flag.Int("count", 100, "Number of mock workers")
	duration    = flag.Duration("duration", 60*time.Second, "Test duration")
)

// 统计指标
var (
	heartbeatSuccess int64
	heartbeatFail    int64
)

func main() {
	flag.Parse()
	log.Printf("🚀 Starting Mock Cluster with %d nodes...", *workerCount)

	var wg sync.WaitGroup
	ctxDone := make(chan struct{})

	// 启动模拟节点
	for i := 0; i < *workerCount; i++ {
		wg.Add(1)
		// 生成唯一的虚拟 IP
		mockIP := fmt.Sprintf("192.168.100.%d", 100+i)
		mockPort := 8000 + i
		go runMockWorker(mockIP, mockPort, &wg, ctxDone)
	}

	// 定时打印统计
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		for {
			select {
			case <-ctxDone:
				return
			case <-ticker.C:
				s := atomic.LoadInt64(&heartbeatSuccess)
				f := atomic.LoadInt64(&heartbeatFail)
				log.Printf("📊 Stats: Success=%d, Fail=%d, Rate=%.1f req/s", s, f, float64(s+f)/2.0)
				// 重置计数以便观察瞬时压力
				atomic.StoreInt64(&heartbeatSuccess, 0)
				atomic.StoreInt64(&heartbeatFail, 0)
			}
		}
	}()

	// 运行指定时间后停止
	time.Sleep(*duration)
	close(ctxDone)
	wg.Wait()
	log.Println("✅ Test Finished.")
}

func runMockWorker(ip string, port int, wg *sync.WaitGroup, done chan struct{}) {
	defer wg.Done()

	// 模拟静态信息
	info := protocol.NodeInfo{
		Hostname:  fmt.Sprintf("mock-node-%s", ip),
		IP:        ip,
		OS:        "linux",
		Arch:      "amd64",
		CPUCores:  4,
		MemTotal:  16384,
		DiskTotal: 500,
	}

	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(3 * time.Second) // 3秒一次心跳
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			// 模拟动态状态
			status := protocol.NodeStatus{
				CPUUsage: rand.Float64() * 100,
				MemUsage: 40.0 + rand.Float64()*20,
				Time:     time.Now().Unix(),
			}

			reqData := protocol.RegisterRequest{
				Port:   port,
				Info:   info,
				Status: status,
			}

			// 发送心跳
			if sendHeartbeat(client, reqData) {
				atomic.AddInt64(&heartbeatSuccess, 1)
			} else {
				atomic.AddInt64(&heartbeatFail, 1)
			}
		}
	}
}

func sendHeartbeat(client *http.Client, data protocol.RegisterRequest) bool {
	jsonData, _ := json.Marshal(data)
	resp, err := client.Post(*masterURL+"/api/worker/heartbeat", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}
