package main

import (
	"fmt"
	"os"
	"time"
)

func main() {
	fmt.Println("=== 文件日志测试 App ===")
	fmt.Printf("进程 ID: %d\n", os.Getpid())

	fileName := "demo_service.log"
	fmt.Printf("开始向 %s 写入日志...\n", fileName)

	f, err := os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("打开文件失败: %v\n", err)
		return
	}
	defer f.Close()

	counter := 0
	for {
		logMsg := fmt.Sprintf("[%s] count=%d | 正在运行业务逻辑...\n", time.Now().Format(time.DateTime), counter)
		if _, err := f.WriteString(logMsg); err != nil {
			fmt.Printf("写入失败: %v\n", err)
		}

		// 同时也输出到标准输出，测试 Worker 的日志重定向功能
		fmt.Print(logMsg)

		counter++
		time.Sleep(1 * time.Second)
	}
}
