package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
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

// ================= 配置参数 =================
var (
	masterURL     = flag.String("master", "http://127.0.0.1:8080", "Master HTTP 地址")
	instanceCount = flag.Int("count", 20, "模拟并发部署的实例数量")
	workerPort    = flag.Int("port", 10000, "Mock Worker 监听端口")
	deployDelay   = flag.Duration("delay", 500*time.Millisecond, "模拟下载解压耗时")
	secret        = flag.String("secret", "ops-system-secret-key", "Auth Token")
)

// ================= 统计指标 =================
var (
	stats struct {
		ReceivedDeploy int64 // 收到部署指令数
		ReceivedAction int64 // 收到启停指令数
		ReportSuccess  int64 // 成功汇报状态数
		ReportFail     int64 // 汇报失败数
	}
	// 本机回环 IP
	nodeIP = "127.0.0.1"
)

func main() {
	flag.Parse()
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	printBanner()

	// 1. 启动单体 Mock Worker (高并发模式)
	log.Printf("🚀 启动 Mock Worker 于 %s:%d ...", nodeIP, *workerPort)
	serverReady := make(chan struct{})
	go startMockWorker(*workerPort, serverReady)
	<-serverReady
	log.Println("✅ Mock Worker 已就绪")

	// 2. 环境初始化 (注册系统、节点、伪造服务包)
	sysID, pkgName, pkgVer, err := setupEnvironment()
	if err != nil {
		log.Fatalf("❌ 环境初始化失败: %v", err)
	}
	log.Printf("✅ 环境准备完成 (SystemID: %s)", sysID)

	// 3. 发起部署风暴 (Trigger Deploy Storm)
	// 即使只有一个节点，Master 也需要处理 N 个并发的 Deploy 请求，并写入 N 条数据库记录
	log.Printf("⚡ 正在向节点 %s 触发 %d 个并发部署请求...", nodeIP, *instanceCount)
	triggerDeployStorm(sysID, pkgName, pkgVer)

	// 4. 保持运行
	log.Println("👀 正在模拟 Worker 下载与状态流转 (Ctrl+C 停止)...")

	ctx, cancel := context.WithCancel(context.Background())
	go monitorStats(ctx)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	cancel()
	log.Println("🛑 测试结束")
}

// ================= Mock Worker =================

func startMockWorker(port int, readyChan chan struct{}) {
	mux := http.NewServeMux()

	// 模拟心跳 (注册自己)
	go func() {
		// 持续发送心跳，确保 Master 认为该节点 Online
		hbData := protocol.RegisterRequest{
			Port: port,
			Info: protocol.NodeInfo{
				IP:       nodeIP,
				Hostname: "stress-node-01",
				OS:       "linux",
				Arch:     "amd64",
				Status:   "online",
			},
			Status: protocol.NodeStatus{CPUUsage: 10.0, MemUsage: 30.0},
		}

		// 首次立即发送
		if err := authPost("/api/worker/heartbeat", hbData); err != nil {
			log.Printf("⚠️ 首次心跳失败 (Master未启动?): %v", err)
		}

		ticker := time.NewTicker(3 * time.Second)
		for range ticker.C {
			_ = authPost("/api/worker/heartbeat", hbData)
		}
	}()

	// 1. 接收部署指令
	mux.HandleFunc("/api/deploy", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&stats.ReceivedDeploy, 1)

		var req protocol.DeployRequest
		json.NewDecoder(r.Body).Decode(&req)

		// 立即响应 Master "OK"
		w.Write([]byte("ok"))

		// 异步模拟耗时操作 (Goroutine)
		go func(instID string) {
			// 阶段 A: 下载/解压中 (Deploying)
			reportStatus(instID, "deploying", 0)

			// 模拟 I/O 耗时
			time.Sleep(*deployDelay)

			// 阶段 B: 部署完成，等待启动 (Stopped)
			reportStatus(instID, "stopped", 0)
		}(req.InstanceID)
	})

	// 2. 接收启停指令
	mux.HandleFunc("/api/instance/action", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&stats.ReceivedAction, 1)
		var req protocol.InstanceActionRequest
		json.NewDecoder(r.Body).Decode(&req)

		w.Write([]byte("ok"))

		go func(instID, action string) {
			time.Sleep(100 * time.Millisecond) // 模拟进程操作
			if action == "start" {
				reportStatus(instID, "running", 8000+int(randInt(1000)))
			} else if action == "stop" {
				reportStatus(instID, "stopped", 0)
			} else if action == "destroy" {
				// destroy 通常不需要回报状态，或者回报已销毁
			}
		}(req.InstanceID, req.Action)
	})

	server := &http.Server{Addr: fmt.Sprintf("0.0.0.0:%d", port), Handler: mux}

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		log.Fatalf("端口 %d 被占用: %v", port, err)
	}

	close(readyChan)
	if err := server.Serve(listener); err != nil {
		log.Printf("Worker Server Error: %v", err)
	}
}

func reportStatus(instID, status string, pid int) {
	data := protocol.InstanceStatusReport{
		InstanceID: instID,
		Status:     status,
		PID:        pid,
		Uptime:     100,
		CpuUsage:   15.5,
		MemUsage:   256,
	}
	err := authPost("/api/instance/status_report", data)
	if err != nil {
		atomic.AddInt64(&stats.ReportFail, 1)
	} else {
		atomic.AddInt64(&stats.ReportSuccess, 1)
	}
}

// ================= Orchestration =================

func setupEnvironment() (string, string, string, error) {
	// 1. 创建系统
	sysName := "DeployStressTest"
	_ = authPost("/api/systems/create", map[string]string{
		"name":        sysName,
		"description": "Auto created by mock_deploy_consumer",
	})

	// 获取 System ID
	sysID, err := getSystemID(sysName)
	if err != nil {
		return "", "", "", fmt.Errorf("get system failed: %v", err)
	}

	// 2. 伪造服务包
	pkgName := "StressApp"
	pkgVer := "v1.0.0"

	manifest := protocol.ServiceManifest{
		Name: pkgName, Version: pkgVer,
		Entrypoint: "app", Description: "Fake package",
		ReadinessType: "none",
	}

	err = authPost("/api/package/callback", map[string]interface{}{
		"manifest": manifest,
		"size":     1024,
		"key":      "fake/path/app.zip",
	})
	if err != nil {
		return "", "", "", fmt.Errorf("fake package failed: %v", err)
	}

	// 3. 添加组件定义 (覆盖默认配置)
	_ = authPost("/api/systems/module/add", protocol.SystemModule{
		SystemID:       sysID,
		ModuleName:     "CoreApp",
		PackageName:    pkgName,
		PackageVersion: pkgVer,
		StartOrder:     1,
	})

	return sysID, pkgName, pkgVer, nil
}

func triggerDeployStorm(sysID, pkgName, pkgVer string) {
	var wg sync.WaitGroup

	for i := 0; i < *instanceCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// 所有请求都发给同一个 NodeIP，但带不同的 InstanceID (由 Master 生成)
			// 这里我们调用 Master 的 Deploy 接口，Master 会生成 InstanceID 并落库
			// 请求体：
			req := struct {
				SystemID       string `json:"system_id"`
				NodeIP         string `json:"node_ip"`
				ServiceName    string `json:"service_name"`
				ServiceVersion string `json:"service_version"`
			}{
				SystemID:       sysID,
				NodeIP:         nodeIP, // 127.0.0.1
				ServiceName:    pkgName,
				ServiceVersion: pkgVer,
			}

			err := authPost("/api/deploy", req)
			if err != nil {
				log.Printf("❌ 部署请求失败 (Req %d): %v", idx, err)
			}
		}(i)
		// 极短间隔，模拟高并发点击
		time.Sleep(5 * time.Millisecond)
	}
	wg.Wait()
}

// ================= Helpers =================

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
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func authGet(path string) ([]byte, error) {
	req, _ := http.NewRequest("GET", *masterURL+path, nil)
	req.Header.Set("Authorization", "Bearer "+*secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func getSystemID(sysName string) (string, error) {
	body, err := authGet("/api/systems")
	if err != nil {
		return "", err
	}

	var res struct {
		Data []struct {
			ID   string
			Name string
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
	return "", fmt.Errorf("not found")
}

func monitorStats(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fmt.Printf("\r📊 [Deploy] Recv: %d | [Action] Recv: %d | [Report] OK: %d / Fail: %d",
				atomic.LoadInt64(&stats.ReceivedDeploy),
				atomic.LoadInt64(&stats.ReceivedAction),
				atomic.LoadInt64(&stats.ReportSuccess),
				atomic.LoadInt64(&stats.ReportFail),
			)
		}
	}
}

func randInt(max int) int64 {
	// 简单的随机数，生产环境建议用 crypto/rand 或 seed
	return int64(time.Now().UnixNano() % int64(max))
}

func printBanner() {
	fmt.Println(`
  ____  _____ _____  _      _____   __  __  ____   _____ _  __
 |  _ \| ____|  __ \| |    / __ \  |  \/  |/ __ \ / ____| |/ /
 | | | | |__ | |__) | |   | |  | | | \  / | |  | | |    | ' / 
 | | | |  __||  ___/| |   | |  | | | |\/| | |  | | |    |  <  
 | |_| | |___| |    | |___| |__| | | |  | | |__| | |____| . \ 
 |____/|_____|_|    |______\____/  |_|  |_|\____/ \_____|_|\_\
                                                              
 >> GDOS Deployment Consumer (Single Node Mode)
`)
}
