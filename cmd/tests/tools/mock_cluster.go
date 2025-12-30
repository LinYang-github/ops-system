package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
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
)

// ==========================================
// 配置参数
// ==========================================
var (
	masterURL   = flag.String("master", "http://127.0.0.1:8080", "Master 服务的地址")
	nodeCount   = flag.Int("count", 200, "模拟节点数量")
	duration    = flag.Duration("duration", 5*time.Minute, "测试持续时间")
	interval    = flag.Duration("interval", 5*time.Second, "初始心跳间隔")
	packetLoss  = flag.Int("loss", 0, "模拟丢包率 (0-100%)")
	maxJitterMs = flag.Int("jitter", 0, "模拟网络抖动最大延迟 (ms)")
)

// ==========================================
// 统计指标
// ==========================================
var (
	stats struct {
		Requests    int64
		Success     int64
		Fail        int64
		TotalLat    int64 // 总延迟 (微秒)
		ActiveNodes int64 // 当前在线模拟节点
	}
	startTime = time.Now()
)

// 全局 HTTP Client (复用连接，避免客户端端口耗尽)
var httpClient *http.Client

func init() {
	// 优化 HTTP Client 设置，模拟高并发场景
	httpClient = &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        1000,
			MaxIdleConnsPerHost: 1000,
			IdleConnTimeout:     90 * time.Second,
			DisableKeepAlives:   false,
			DialContext: (&net.Dialer{
				Timeout:   2 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}
}

func main() {
	flag.Parse()

	printBanner()

	// 信号处理 (优雅退出)
	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 启动统计监控协程
	go monitorStats(ctx)

	// 启动模拟节点
	var wg sync.WaitGroup
	log.Printf("🚀 正在启动 %d 个模拟节点...", *nodeCount)
	log.Printf("🎯 目标 Master: %s", *masterURL)

	// 限制并发启动速度，避免瞬间把 Master 甚至本机打死
	startTicker := time.NewTicker(10 * time.Millisecond)
	for i := 0; i < *nodeCount; i++ {
		select {
		case <-ctx.Done():
			break
		case <-sigChan:
			cancel()
			goto CLEANUP
		case <-startTicker.C:
			wg.Add(1)
			// 生成确定性的 IP，方便多次测试对比
			// 10.x.x.x
			mockIP := fmt.Sprintf("10.%d.%d.%d", (i/65536)%255, (i/256)%255, i%255+1)
			mockName := fmt.Sprintf("load-test-worker-%04d", i)

			go func(ip, name string, offset int) {
				defer wg.Done()
				runMockWorker(ctx, ip, name, offset)
			}(mockIP, mockName, i)
		}
	}
	startTicker.Stop()

	// 等待结束信号
	select {
	case <-ctx.Done():
		log.Println("\n⏱️ 测试时间结束")
	case <-sigChan:
		log.Println("\n🛑 接收到停止信号")
		cancel()
	}

CLEANUP:
	log.Println("正在等待所有协程退出...")
	wg.Wait()
	printFinalReport()
}

// ==========================================
// 模拟 Worker 逻辑
// ==========================================

func runMockWorker(ctx context.Context, ip, name string, offset int) {
	atomic.AddInt64(&stats.ActiveNodes, 1)
	defer atomic.AddInt64(&stats.ActiveNodes, -1)

	// 1. 初始化静态信息
	info := protocol.NodeInfo{
		IP:        ip,
		Port:      8081, // 模拟端口
		Hostname:  name,
		Name:      name,
		OS:        "linux",
		Arch:      "amd64",
		CPUCores:  8,
		MemTotal:  32 * 1024, // 32GB
		DiskTotal: 500 * 1024 * 1024 * 1024,
		MacAddr:   fmt.Sprintf("52:54:00:%02x:%02x:%02x", rand.Intn(255), rand.Intn(255), rand.Intn(255)),
	}

	// 初始间隔
	currentInterval := *interval
	ticker := time.NewTicker(currentInterval)
	defer ticker.Stop()

	// 模拟负载的正弦波相位偏移，让不同节点的波峰错开
	phaseShift := float64(offset) * 0.1

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 2. 模拟丢包
			if *packetLoss > 0 && rand.Intn(100) < *packetLoss {
				continue // 跳过本次心跳
			}

			// 3. 模拟网络抖动 (Sleep)
			if *maxJitterMs > 0 {
				jitter := time.Duration(rand.Intn(*maxJitterMs)) * time.Millisecond
				time.Sleep(jitter)
			}

			// 4. 生成动态 Metrics (正弦波 + 随机噪点)
			now := float64(time.Now().Unix())
			// CPU: 基础值 20% + 波动幅度 30% * sin(t) + 随机噪点
			cpuLoad := 20.0 + 30.0*math.Sin(now/60.0+phaseShift) + rand.Float64()*10
			if cpuLoad < 0 {
				cpuLoad = 0
			}
			if cpuLoad > 100 {
				cpuLoad = 100
			}

			// Mem: 类似，但周期更长
			memLoad := 40.0 + 20.0*math.Sin(now/300.0+phaseShift) + rand.Float64()*5

			status := protocol.NodeStatus{
				CPUUsage:    cpuLoad,
				MemUsage:    memLoad,
				DiskUsage:   50.0,
				NetInSpeed:  rand.Float64() * 1024, // 1MB/s range
				NetOutSpeed: rand.Float64() * 2048,
				Uptime:      uint64(time.Since(startTime).Seconds()),
				Time:        time.Now().Unix(),
			}

			reqData := protocol.RegisterRequest{
				Port:   8081,
				Info:   info,
				Status: status,
			}

			// 5. 发送请求
			start := time.Now()
			newInterval, err := sendHeartbeat(reqData)
			latency := time.Since(start).Microseconds()

			// 6. 更新统计
			atomic.AddInt64(&stats.Requests, 1)
			atomic.AddInt64(&stats.TotalLat, latency)
			if err != nil {
				atomic.AddInt64(&stats.Fail, 1)
				// 简单的错误日志限流
				if rand.Float32() < 0.01 {
					log.Printf("Worker %s heartbeat error: %v", name, err)
				}
			} else {
				atomic.AddInt64(&stats.Success, 1)

				// 7. 处理 Master 下发的动态配置
				// 如果 Master 要求改变心跳频率，这里模拟 Worker 的调整行为
				if newInterval > 0 && newInterval != int64(currentInterval.Seconds()) {
					currentInterval = time.Duration(newInterval) * time.Second
					ticker.Reset(currentInterval)
				}
			}
		}
	}
}

// sendHeartbeat 发送心跳并返回 Master 要求的新间隔
func sendHeartbeat(data protocol.RegisterRequest) (int64, error) {
	payload, _ := json.Marshal(data)

	req, _ := http.NewRequest("POST", *masterURL+"/api/worker/heartbeat", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	// 模拟鉴权
	req.Header.Set("Authorization", "Bearer ops-system-secret-key")

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	// 读取响应以复用连接
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("status code %d", resp.StatusCode)
	}

	// 解析响应中的动态配置
	var result struct {
		Code int `json:"code"`
		Data struct {
			HeartbeatInterval int64 `json:"heartbeat_interval"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, nil // 解析失败忽略，不当做心跳失败
	}

	return result.Data.HeartbeatInterval, nil
}

// ==========================================
// 辅助函数
// ==========================================

func monitorStats(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastReqs int64 = 0

	fmt.Println("\n📊 实时监控数据 (Press Ctrl+C to stop)")
	fmt.Printf("%-10s | %-10s | %-8s | %-8s | %-8s\n", "Nodes", "QPS", "Succ", "Fail", "AvgLat")
	fmt.Println("----------------------------------------------------------")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			currReqs := atomic.LoadInt64(&stats.Requests)
			currSucc := atomic.LoadInt64(&stats.Success)
			currFail := atomic.LoadInt64(&stats.Fail)
			currLatTotal := atomic.LoadInt64(&stats.TotalLat)
			active := atomic.LoadInt64(&stats.ActiveNodes)

			qps := currReqs - lastReqs
			avgLat := 0.0
			if qps > 0 {
				// 计算这1秒内的平均延迟 (这只是一个近似值，更精确的需要用直方图)
				// 注意：TotalLat 是累积值，这里计算会有偏差，为了简单展示暂且如此
				// 更好的做法是 reset atomic counter，但有并发问题。
				// 作为一个简单 Mock 工具，我们直接算整体平均值
				if currReqs > 0 {
					avgLat = float64(currLatTotal) / float64(currReqs) / 1000.0 // ms
				}
			}

			fmt.Printf("\r%-10d | %-10d | %-8d | %-8d | %-8.2f ms",
				active, qps, currSucc, currFail, avgLat)

			lastReqs = currReqs
		}
	}
}

func printBanner() {
	fmt.Println(`
   ___  ___  ___  _____   __  ___  ___  _____  __ 
  / _ \/ _ \/ _ \/ __/ | / / / _ \/ _ \/ __/ |/ /
 / // / // / // /\ \ | |/ / / // / ___/\ \/    / 
/____/____/\___/___/ |___/ /____/_/  /___/_/|_|  
                                                 
>> GDOS Mock Cluster Load Tester
	`)
}

func printFinalReport() {
	durationSec := time.Since(startTime).Seconds()
	total := atomic.LoadInt64(&stats.Requests)

	fmt.Println("\n\n📋 测试报告 summary")
	fmt.Println("========================================")
	fmt.Printf("总耗时:      %.2f s\n", durationSec)
	fmt.Printf("总请求数:    %d\n", total)
	fmt.Printf("成功请求:    %d\n", atomic.LoadInt64(&stats.Success))
	fmt.Printf("失败请求:    %d\n", atomic.LoadInt64(&stats.Fail))
	fmt.Printf("平均 QPS:    %.2f\n", float64(total)/durationSec)
	fmt.Println("========================================")
}
