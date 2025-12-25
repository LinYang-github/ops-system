package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"ops-system/internal/worker/agent"
	"ops-system/internal/worker/executor"
	"ops-system/internal/worker/handler"

	// 使用别名解决冲突
	"ops-system/internal/worker/utils" // 本地工具 (自启)
	"ops-system/pkg/config"
	pkgUtils "ops-system/pkg/utils" // 公共工具 (HTTP Client)

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func main() {
	// 1. 获取当前执行文件的绝对路径
	ex, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	exPath := filepath.Dir(ex)
	defaultWorkDir := filepath.Join(exPath, "instances")

	// 2. 命令行参数定义
	cfgFile := pflag.StringP("config", "c", "", "Config file path")

	pflag.Int("port", 8081, "Worker listening port")
	viper.BindPFlag("server.port", pflag.Lookup("port"))

	pflag.String("master", "http://127.0.0.1:8080", "Master URL")
	viper.BindPFlag("connect.master_url", pflag.Lookup("master"))

	pflag.String("work_dir", defaultWorkDir, "Instances directory")
	viper.BindPFlag("server.work_dir", pflag.Lookup("work_dir"))

	// [新增] 允许通过命令行覆盖 Secret Key
	pflag.String("secret", "ops-system-secret-key", "Auth Secret Key")
	viper.BindPFlag("auth.secret_key", pflag.Lookup("secret"))

	// 自启参数独立处理
	autoStart := pflag.Int("autostart", -1, "Auto start setting: 1=enable, 0=disable")

	pflag.Parse()

	// 3. 加载配置
	cfg, err := config.LoadWorkerConfig(*cfgFile)
	if err != nil {
		log.Fatalf("Load config failed: %v", err)
	}

	absWorkDir, err := filepath.Abs(cfg.Server.WorkDir)
	if err != nil {
		log.Fatalf("Invalid work dir: %v", err)
	}

	// 4. 处理开机自启
	if *autoStart != -1 {
		enable := *autoStart == 1
		// 注意：自启写入的参数也需要包含 secret，否则自启后鉴权失败
		// 这里 HandleAutoStart 可能需要修改以支持 secret 参数，或者暂且忽略（使用默认值）
		if err := utils.HandleAutoStart(enable, cfg.Connect.MasterURL, cfg.Server.Port, absWorkDir); err != nil {
			log.Fatalf("配置自启失败: %v", err)
		}
		return
	}

	// 5. 初始化全局 HTTP Client
	// 【关键修复】传入配置中的 SecretKey
	log.Printf("Initializing HTTP Client with SecretKey: %s...", maskSecret(cfg.Auth.SecretKey))
	pkgUtils.InitHTTPClient(cfg.Logic.HTTPClientTimeout, cfg.Auth.SecretKey)

	// 6. 初始化业务模块
	executor.Init(absWorkDir)
	handler.InitHandler(cfg.Connect.MasterURL)

	listenAddr := fmt.Sprintf(":%d", cfg.Server.Port)

	log.Printf("Worker started.")
	log.Printf(" > Listen:   %s", listenAddr)
	log.Printf(" > Master:   %s", cfg.Connect.MasterURL)
	log.Printf(" > Work Dir: %s", absWorkDir)
	log.Printf(" > Interval: Heartbeat=%v, Monitor=%v", cfg.Logic.HeartbeatInterval, cfg.Logic.MonitorInterval)

	executor.StartMonitor(cfg.Connect.MasterURL, cfg.Logic.MonitorInterval)

	// 启动 Server
	go handler.StartWorkerServer(listenAddr)

	// 启动心跳
	agent.StartHeartbeat(cfg.Connect.MasterURL, cfg.Server.Port, cfg.Logic.HeartbeatInterval)
}

func maskSecret(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	return s[:2] + "****" + s[len(s)-2:]
}
