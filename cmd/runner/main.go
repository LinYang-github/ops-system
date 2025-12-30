package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"

	"ops-system/pkg/protocol"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: runner [start|stop]")
		return
	}
	action := os.Args[1]

	// 1. 读取清单
	manifestBytes, err := os.ReadFile("manifest.json")
	if err != nil {
		log.Fatalf("Missing manifest.json: %v", err)
	}
	var manifest protocol.RunnerManifest
	json.Unmarshal(manifestBytes, &manifest)

	fmt.Printf(">>> System: %s (Exported: %d)\n", manifest.SystemName, manifest.ExportTime)

	if action == "start" {
		startSystem(manifest)
	} else if action == "stop" {
		stopSystem(manifest)
	}
}

// 启动逻辑
func startSystem(m protocol.RunnerManifest) {
	// 按 Priority 分组
	groups := make(map[int][]protocol.RunnerModule)
	var orders []int
	for _, mod := range m.Modules {
		if _, ok := groups[mod.StartOrder]; !ok {
			orders = append(orders, mod.StartOrder)
		}
		groups[mod.StartOrder] = append(groups[mod.StartOrder], mod)
	}
	sort.Ints(orders)

	// 顺序启动
	for _, order := range orders {
		fmt.Printf("\n--- Starting Group Priority %d ---\n", order)
		var wg sync.WaitGroup

		for _, mod := range groups[order] {
			wg.Add(1)
			go func(module protocol.RunnerModule) {
				defer wg.Done()
				if err := startProcess(module); err != nil {
					log.Printf("❌ [%s] Start failed: %v", module.Name, err)
				} else {
					log.Printf("✅ [%s] Started.", module.Name)
				}
			}(mod)
		}
		wg.Wait()

		// 组间简单延时
		time.Sleep(2 * time.Second)
	}

	fmt.Println("\n>>> All services started. Press Ctrl+C to stop.")

	// 阻塞直到信号
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	fmt.Println("\n>>> Shutting down...")
	stopSystem(m)
}

func startProcess(mod protocol.RunnerModule) error {
	// 绝对路径
	absWorkDir, _ := filepath.Abs(mod.WorkDir)
	cmdPath := filepath.Join(absWorkDir, mod.Entrypoint)

	// 这里为了简单，直接使用 exec.Command
	// 实际可以复用 executor 的 resolveExecutable 逻辑处理权限
	if _, err := os.Stat(cmdPath); err != nil {
		return err
	}

	cmd := exec.Command(cmdPath, mod.Args...)
	cmd.Dir = absWorkDir

	// 注入 Env
	cmd.Env = os.Environ()
	for k, v := range mod.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// 日志
	logFile, _ := os.Create(filepath.Join(absWorkDir, "runner.log"))
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		return err
	}

	// 写入 PID (用于 Stop)
	os.WriteFile(filepath.Join(absWorkDir, "pid"), []byte(fmt.Sprintf("%d", cmd.Process.Pid)), 0644)

	return nil
}

func stopSystem(m protocol.RunnerManifest) {
	// 逆序停止 (这里简单并发全停)
	for _, mod := range m.Modules {
		pidPath := filepath.Join(mod.WorkDir, "pid")
		data, err := os.ReadFile(pidPath)
		if err == nil {
			pid, _ := strconv.Atoi(string(data))
			if proc, err := os.FindProcess(pid); err == nil {
				log.Printf("Killing [%s] PID: %d", mod.Name, pid)
				proc.Kill()
			}
			os.Remove(pidPath)
		}
	}
}
