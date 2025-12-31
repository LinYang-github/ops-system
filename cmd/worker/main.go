package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"ops-system/internal/worker/executor"
	"ops-system/internal/worker/handler"
	"ops-system/internal/worker/transport" // [新增] 引入 Transport
	"ops-system/internal/worker/utils"
	"ops-system/pkg/config"
	pkgUtils "ops-system/pkg/utils"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func main() {
	// 1. 基础路径与参数处理
	ex, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	exPath := filepath.Dir(ex)
	defaultWorkDir := filepath.Join(exPath, "instances")

	cfgFile := pflag.StringP("config", "c", "", "Config file path")
	pflag.Int("port", 8081, "Worker listening port")
	viper.BindPFlag("server.port", pflag.Lookup("port"))

	pflag.String("master", "http://127.0.0.1:8080", "Master URL")
	viper.BindPFlag("connect.master_url", pflag.Lookup("master"))

	pflag.String("work_dir", defaultWorkDir, "Instances directory")
	viper.BindPFlag("server.work_dir", pflag.Lookup("work_dir"))

	pflag.String("secret", "ops-system-secret-key", "Auth Secret Key")
	viper.BindPFlag("auth.secret_key", pflag.Lookup("secret"))

	autoStart := pflag.Int("autostart", -1, "Auto start setting")
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

	// 3. 处理自启
	if *autoStart != -1 {
		enable := *autoStart == 1
		if err := utils.HandleAutoStart(enable, cfg.Connect.MasterURL, cfg.Server.Port, absWorkDir); err != nil {
			log.Fatalf("配置自启失败: %v", err)
		}
		return
	}

	// 【新增】初始化节点唯一 ID
	nodeID, err := utils.InitNodeID(absWorkDir)
	if err != nil {
		log.Fatalf("Failed to generate NodeID: %v", err)
	}
	log.Printf("🔹 Worker Identity: %s", nodeID)

	// 4. 初始化
	pkgUtils.InitHTTPClient(cfg.Logic.HTTPClientTimeout, cfg.Auth.SecretKey)
	executor.Init(absWorkDir, cfg.LogRotate)
	handler.InitHandler(cfg.Connect.MasterURL)

	log.Printf("Worker started.")
	log.Printf(" > Listen:   :%d", cfg.Server.Port)
	log.Printf(" > Master:   %s", cfg.Connect.MasterURL)
	log.Printf(" > Work Dir: %s", absWorkDir)

	// 5. [核心变更] 启动 WebSocket Client (替代旧的 agent.StartHeartbeat)
	// 这会建立长连接，并在连接成功后自动发送 Register/Heartbeat 包
	transport.StartClient(cfg.Connect.MasterURL, cfg.Auth.SecretKey)

	// 6. 启动本地监控采集 (依然需要，用于定期上报状态)
	// 注意：Monitor 内部现在是通过 transport 还是 http 上报取决于 executor 的实现
	// 建议暂时保留 executor 的独立监控逻辑，或者后续将其合并到 transport 中
	executor.StartMonitor(cfg.Connect.MasterURL, cfg.Logic.MonitorInterval)

	// 7. 启动 Worker 自身的 HTTP 服务 (用于日志查看、本地调试等)
	// 注意：现在指令通过 WS 下发，但 Log Stream 可能还依赖 HTTP
	handler.StartWorkerServer(fmt.Sprintf(":%d", cfg.Server.Port))
}

func maskSecret(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	return s[:2] + "****" + s[len(s)-2:]
}
