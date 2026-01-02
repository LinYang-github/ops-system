package main

import (
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"ops-system/internal/worker/executor"
	"ops-system/internal/worker/transport"
	"ops-system/internal/worker/utils"
	"ops-system/pkg/config"
	"ops-system/pkg/protocol"
	pkgUtils "ops-system/pkg/utils"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func main() {
	// 1. 基础路径与参数解析
	ex, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	exPath := filepath.Dir(ex)
	defaultWorkDir := filepath.Join(exPath, "instances")

	cfgFile := pflag.StringP("config", "c", "", "Config file path")

	// [移除] -port 参数
	// pflag.Int("port", 8081, "Worker listening port")

	pflag.String("master", "http://127.0.0.1:8080", "Master URL")
	viper.BindPFlag("connect.master_url", pflag.Lookup("master"))

	pflag.String("work_dir", defaultWorkDir, "Instances directory")
	viper.BindPFlag("server.work_dir", pflag.Lookup("work_dir"))

	pflag.String("secret", "ops-system-secret-key", "Auth Secret Key")
	viper.BindPFlag("auth.secret_key", pflag.Lookup("secret"))

	autoStart := pflag.Int("autostart", -1, "Auto start setting (1=enable, 0=disable)")
	pflag.Parse()

	// 2. 加载配置
	cfg, err := config.LoadWorkerConfig(*cfgFile)
	if err != nil {
		log.Fatalf("Load config failed: %v", err)
	}

	absWorkDir, err := filepath.Abs(cfg.Server.WorkDir)
	if err != nil {
		log.Fatalf("Invalid work dir: %v", err)
	}

	// 3. 处理开机自启 (移除 Port 参数传递)
	if *autoStart != -1 {
		enable := *autoStart == 1
		// [修改] HandleAutoStart 签名需调整，不再传 port
		if err := utils.HandleAutoStart(enable, cfg.Connect.MasterURL, absWorkDir); err != nil {
			log.Fatalf("配置自启失败: %v", err)
		}
		return
	}

	// 4. 初始化节点身份
	nodeID, err := utils.InitNodeID(absWorkDir)
	if err != nil {
		log.Fatalf("Failed to generate NodeID: %v", err)
	}

	// 初始化通用 HTTP 客户端 (仅用于 Worker 下载 Master 的文件)
	pkgUtils.InitHTTPClient(cfg.Logic.HTTPClientTimeout, cfg.Auth.SecretKey)

	// 5. 核心组件初始化
	// A. 初始化执行器
	execMgr := executor.NewManager(absWorkDir, cfg.LogRotate)

	// B. 初始化传输层 (WebSocket Client)
	wsClient := transport.StartClient(cfg.Connect.MasterURL, cfg.Auth.SecretKey, execMgr)

	// C. 配置 Executor 的上报回调
	execMgr.SetStatusReporter(func(report protocol.InstanceStatusReport) {
		if wsClient != nil {
			wsClient.SendStatusReport(report)
		}
	})

	// D. 启动本地监控
	execMgr.StartMonitor(cfg.Logic.MonitorInterval)

	// [移除] HTTP Handler 初始化
	// httpHandler := handler.NewWorkerHandler(execMgr)
	// httpHandler.RegisterRoutes()

	log.Printf("Worker started (Pure WebSocket Mode).")
	log.Printf(" > Node ID:  %s", nodeID)
	log.Printf(" > Master:   %s", cfg.Connect.MasterURL)
	log.Printf(" > Work Dir: %s", absWorkDir)

	// 6. 阻塞主进程
	// 由于移除了 HTTP Server，主进程需要手动阻塞，否则会直接退出
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Worker shutting down...")
}
