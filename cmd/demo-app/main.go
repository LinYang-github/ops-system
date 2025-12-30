package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// 1. 解析命令行参数 (验证 args 注入)
	port := flag.String("port", "9090", "HTTP监听端口")
	name := flag.String("name", "DemoApp", "服务名称")
	flag.Parse()

	// 设置日志前缀
	log.SetPrefix(fmt.Sprintf("[%s] ", *name))
	log.SetFlags(log.Ldate | log.Ltime)

	log.Printf("=== 启动 Demo App ===")
	log.Printf("PID: %d", os.Getpid())
	log.Printf("监听端口: %s", *port)

	// 2. 打印环境变量 (验证 env 注入)
	log.Println("--- 环境变量检查 ---")
	targetEnvs := []string{"GIN_MODE", "DB_HOST", "MY_CUSTOM_VAR"}
	for _, key := range targetEnvs {
		val := os.Getenv(key)
		if val != "" {
			log.Printf("%s = %s", key, val)
		} else {
			log.Printf("%s = (未设置)", key)
		}
	}
	log.Println("--------------------")

	// 3. 启动一个后台协程，模拟业务日志输出
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for t := range ticker.C {
			log.Printf("心跳正常: %s | 正在处理业务逻辑...", t.Format("15:04:05"))
		}
	}()

	// 4. 启动 HTTP 服务 (阻塞主进程，使其不退出)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		msg := fmt.Sprintf("Hello from %s! Running on port %s", *name, *port)
		fmt.Fprintf(w, msg)
		log.Printf("收到请求: %s %s", r.Method, r.URL.Path)
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("收到health请求: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// 5. 优雅退出处理 (可选，但在运维系统中很重要)
	// 创建一个通道接收信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// 在另一个协程启动 Server，避免 ListenAndServe 阻塞信号处理
	go func() {
		addr := ":" + *port
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Fatalf("HTTP Server 启动失败: %v", err)
		}
	}()

	// 阻塞等待停止信号
	sig := <-quit
	log.Printf("接收到停止信号: %v，正在清理资源并退出...", sig)
	time.Sleep(1 * time.Second) // 模拟清理耗时
	log.Println("Bye Bye!")
}
