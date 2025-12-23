package main

import (
	"fmt"
	"os"
	"time"
)

func main() {
	fmt.Println("=== 短作业测试 App ===")
	fmt.Printf("进程 ID: %d\n", os.Getpid())

	// 打印接收到的参数
	fmt.Println("接收到的参数:")
	for i, arg := range os.Args {
		fmt.Printf("  Arg[%d]: %s\n", i, arg)
	}

	// 打印特定环境变量
	fmt.Printf("环境变量 TEST_ENV: %s\n", os.Getenv("TEST_ENV"))

	fmt.Println("正在模拟任务处理 (2秒)...")
	time.Sleep(2 * time.Second)
	fmt.Println("任务完成，正常退出。")
}
