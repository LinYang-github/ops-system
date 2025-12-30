package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
)

func main() {
	port := flag.String("port", "8090", "HTTP监听端口")
	flag.Parse()

	fmt.Println("=== 网络服务测试 App ===")
	fmt.Printf("进程 ID: %d\n", os.Getpid())

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		hostname, _ := os.Hostname()
		fmt.Fprintf(w, "Hello from %s! (PID: %d)", hostname, os.Getpid())
		fmt.Printf("收到请求: %s\n", r.RemoteAddr)
	})

	addr := ":" + *port
	fmt.Printf("正在监听端口 %s ...\n", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		fmt.Printf("启动失败: %v\n", err)
		os.Exit(1)
	}
}
