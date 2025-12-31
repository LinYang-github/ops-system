package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"ops-system/internal/worker/executor"
	"ops-system/internal/worker/handler"
	"ops-system/internal/worker/transport"
	"ops-system/internal/worker/utils"
	"ops-system/pkg/config"
	"ops-system/pkg/protocol"
	pkgUtils "ops-system/pkg/utils"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func main() {
	// ========================================================
	// 1. 基础路径与参数解析
	// ========================================================
	ex, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	exPath := filepath.Dir(ex)
	defaultWorkDir := filepath.Join(exPath, "instances")

	// 定义命令行参数
	cfgFile := pflag.StringP("config", "c", "", "Config file path")
	pflag.Int("port", 8081, "Worker listening port")
	viper.BindPFlag("server.port", pflag.Lookup("port"))

	pflag.String("master", "http://127.0.0.1:8080", "Master URL")
	viper.BindPFlag("connect.master_url", pflag.Lookup("master"))

	pflag.String("work_dir", defaultWorkDir, "Instances directory")
	viper.BindPFlag("server.work_dir", pflag.Lookup("work_dir"))

	pflag.String("secret", "ops-system-secret-key", "Auth Secret Key")
	viper.BindPFlag("auth.secret_key", pflag.Lookup("secret"))

	autoStart := pflag.Int("autostart", -1, "Auto start setting (1=enable, 0=disable)")
	pflag.Parse()

	// ========================================================
	// 2. 加载配置
	// ========================================================
	cfg, err := config.LoadWorkerConfig(*cfgFile)
	if err != nil {
		log.Fatalf("Load config failed: %v", err)
	}

	absWorkDir, err := filepath.Abs(cfg.Server.WorkDir)
	if err != nil {
		log.Fatalf("Invalid work dir: %v", err)
	}

	// ========================================================
	// 3. 处理开机自启 (特权操作，独立流程)
	// ========================================================
	if *autoStart != -1 {
		enable := *autoStart == 1
		if err := utils.HandleAutoStart(enable, cfg.Connect.MasterURL, cfg.Server.Port, absWorkDir); err != nil {
			log.Fatalf("配置自启失败: %v", err)
		}
		return
	}

	// ========================================================
	// 4. 初始化节点身份
	// ========================================================
	nodeID, err := utils.InitNodeID(absWorkDir)
	if err != nil {
		log.Fatalf("Failed to generate NodeID: %v", err)
	}

	// 初始化通用 HTTP 客户端 (用于非 WS 的请求)
	pkgUtils.InitHTTPClient(cfg.Logic.HTTPClientTimeout, cfg.Auth.SecretKey)

	// ========================================================
	// 5. [核心重构] 依赖注入与组件初始化
	// ========================================================

	// A. 初始化执行器管理器 (持有所有本地状态)
	execMgr := executor.NewManager(absWorkDir, cfg.LogRotate)

	// B. 初始化传输层 (WebSocket Client)
	// 将 execMgr 注入 Client，以便收到 Master 指令时调用 Executor
	wsClient := transport.StartClient(cfg.Connect.MasterURL, cfg.Auth.SecretKey, execMgr)

	// C. 配置 Executor 的上报回调
	// 当 Executor 监控到状态变化时，通过此闭包调用 wsClient 发送数据
	// 这解耦了 Executor 对 Transport 的直接依赖
	execMgr.SetStatusReporter(func(report protocol.InstanceStatusReport) {
		if wsClient != nil {
			wsClient.SendStatusReport(report)
		}
	})

	// D. 启动 Executor 内部的监控协程
	execMgr.StartMonitor(cfg.Logic.MonitorInterval)

	// E. 初始化 HTTP 处理器 (用于日志流、文件上传等辅助接口)
	// 注入 execMgr 以便 Handler 操作实例
	httpHandler := handler.NewWorkerHandler(execMgr)

	// 注册路由到 DefaultServeMux
	httpHandler.RegisterRoutes()

	// ========================================================
	// 6. 启动服务
	// ========================================================
	log.Printf("Worker started.")
	log.Printf(" > Node ID:  %s", nodeID)
	log.Printf(" > Listen:   :%d", cfg.Server.Port)
	log.Printf(" > Master:   %s", cfg.Connect.MasterURL)
	log.Printf(" > Work Dir: %s", absWorkDir)

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}
