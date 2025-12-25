package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"ops-system/internal/worker/agent"
	"ops-system/internal/worker/executor"
	"ops-system/internal/worker/handler"
	"ops-system/internal/worker/utils"
	"ops-system/pkg/config"
	pkgUtils "ops-system/pkg/utils"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func main() {
	// 1. 获取当前执行文件的绝对路径 (关键修改)
	// 这样 instances 目录永远生成在 worker.exe 旁边
	ex, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	exPath := filepath.Dir(ex)

	// 2. 计算默认工作目录
	defaultWorkDir := filepath.Join(exPath, "instances")

	// 命令行参数
	cfgFile := pflag.StringP("config", "c", "", "Config file")

	pflag.Int("port", 8081, "Worker port")
	viper.BindPFlag("server.port", pflag.Lookup("port"))

	pflag.String("master", "http://127.0.0.1:8080", "Master URL")
	viper.BindPFlag("connect.master_url", pflag.Lookup("master"))

	pflag.String("work_dir", defaultWorkDir, "Instances directory")
	viper.BindPFlag("server.work_dir", pflag.Lookup("work_dir"))

	// 自启参数依然独立处理
	autoStart := pflag.Int("autostart", -1, "Auto start setting")

	pflag.Parse()

	// 加载配置
	cfg, err := config.LoadWorkerConfig(*cfgFile)
	if err != nil {
		log.Fatalf("Load config failed: %v", err)
	}

	// 4. 再次确保是绝对路径 (防止用户通过命令行传入相对路径如 ./data)
	absWorkDir, err := filepath.Abs(cfg.Server.WorkDir)
	if err != nil {
		log.Fatalf("Invalid work dir: %v", err)
	}
	if *autoStart != -1 {
		enable := *autoStart == 1
		if err := utils.HandleAutoStart(enable, cfg.Connect.MasterURL, cfg.Server.Port, absWorkDir); err != nil {
			log.Fatalf("配置自启失败: %v", err)
		}
	}
	// 初始化全局 HTTP Client
	pkgUtils.InitHTTPClient(cfg.Logic.HTTPClientTimeout, cfg.Auth.SecretKey)

	// 5. 初始化各模块
	executor.Init(absWorkDir)
	handler.InitHandler(cfg.Connect.MasterURL)

	listenAddr := fmt.Sprintf(":%d", cfg.Server.Port)

	log.Printf("Worker started.")
	log.Printf(" > Executable: %s", ex)
	log.Printf(" > Listen:     %s", listenAddr)
	log.Printf(" > Master:     %s", cfg.Connect.MasterURL)
	log.Printf(" > Work Dir:   %s", absWorkDir)

	// 6. 启动监控协程
	executor.StartMonitor(cfg.Connect.MasterURL)

	// 7. 启动 HTTP Server (接收指令)
	go handler.StartWorkerServer(listenAddr)

	// 8. 启动心跳 (上报状态)
	agent.StartHeartbeat(cfg.Connect.MasterURL, cfg.Server.Port)
}
