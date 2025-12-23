package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"ops-system/internal/worker/agent"
	"ops-system/internal/worker/executor"
	"ops-system/internal/worker/handler"
	"ops-system/internal/worker/utils"
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

	// 3. 定义命令行参数
	port := flag.Int("port", 8081, "Worker listening port")
	masterAddr := flag.String("master", "http://127.0.0.1:8080", "Master URL")
	workDir := flag.String("work_dir", defaultWorkDir, "Directory to store instances")

	// 【新增】自启配置参数: 1=开启, 0=关闭, -1(默认)=忽略
	autoStart := flag.Int("autostart", -1, "Set auto-start: 1=enable, 0=disable")

	flag.Parse()

	// 4. 再次确保是绝对路径 (防止用户通过命令行传入相对路径如 ./data)
	absWorkDir, err := filepath.Abs(*workDir)
	if err != nil {
		log.Fatalf("Invalid work dir: %v", err)
	}
	if *autoStart != -1 {
		enable := *autoStart == 1
		if err := utils.HandleAutoStart(enable, *masterAddr, *port, absWorkDir); err != nil {
			log.Fatalf("配置自启失败: %v", err)
		}
	}

	// 5. 初始化各模块
	executor.Init(absWorkDir)
	handler.InitHandler(*masterAddr)

	listenAddr := fmt.Sprintf(":%d", *port)

	log.Printf("Worker started.")
	log.Printf(" > Executable: %s", ex)
	log.Printf(" > Listen:     %s", listenAddr)
	log.Printf(" > Master:     %s", *masterAddr)
	log.Printf(" > Work Dir:   %s", absWorkDir)

	// 6. 启动监控协程
	executor.StartMonitor(*masterAddr)

	// 7. 启动 HTTP Server (接收指令)
	go handler.StartWorkerServer(listenAddr)

	// 8. 启动心跳 (上报状态)
	agent.StartHeartbeat(*masterAddr, *port)
}
