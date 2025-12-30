package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec" // 用于执行系统命令
	"runtime" // 用于判断操作系统
	"time"

	"ops-system/internal/worker/executor"
	"ops-system/pkg/protocol"
)

var masterBaseURL string // 存储 Master 地址

// InitHandler 初始化 Handler，传入 Master 地址
func InitHandler(url string) {
	masterBaseURL = url
}

// StartWorkerServer 启动 Worker HTTP Server
func StartWorkerServer(port string) {
	http.HandleFunc("/api/exec", handleExec)
	http.HandleFunc("/api/terminal/ws", HandleTerminal)
	http.HandleFunc("/api/deploy", handleDeploy)
	http.HandleFunc("/api/instance/action", handleInstanceAction) // 处理实例启停
	http.HandleFunc("/api/external/register", handleRegisterExternal)
	http.HandleFunc("/api/maintenance/cleanup_cache", handleCleanupCache)

	http.HandleFunc("/api/log/ws", handleLogStream)
	http.HandleFunc("/api/log/files", handleGetLogFiles)
	log.Printf("Worker HTTP Server started on %s", port)
	http.ListenAndServe(port, nil)
}

// handleRegisterExternal 处理纳管服务注册 (新增)
func handleRegisterExternal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}

	var req protocol.RegisterExternalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", 400)
		return
	}

	// 调用 executor 在 instances/external 下生成配置和目录
	if err := executor.RegisterExternal(req); err != nil {
		log.Printf("[External] Register failed: %v", err)
		http.Error(w, err.Error(), 500)
		return
	}

	log.Printf("[External] Registered successfully: %s", req.Config.Name)
	w.Write([]byte(`{"status":"ok"}`))
}

// handleDeploy 部署实例 (异步化)
func handleDeploy(w http.ResponseWriter, r *http.Request) {
	var req protocol.DeployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	// 1. 立即返回 OK，不让 Master 等待
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))

	// 2. 启动协程在后台执行耗时操作 (下载、解压)
	go func() {
		// 可选：再次确认上报 deploying (防止 Master 那边没置位)
		executor.ReportStatus(req.InstanceID, "deploying", 0, 0)

		// 执行下载解压
		if err := executor.DeployInstance(req); err != nil {
			log.Printf("[Deploy Error] %v", err)
			// 失败：上报 error
			executor.ReportStatus(req.InstanceID, "error", 0, 0)
		} else {
			log.Printf("[Deploy Success] %s", req.InstanceID)
			// 成功：上报 stopped (表示已就绪，等待启动)
			executor.ReportStatus(req.InstanceID, "stopped", 0, 0)
		}
	}()
}

// handleInstanceAction 处理实例启停
func handleInstanceAction(w http.ResponseWriter, r *http.Request) {
	var req protocol.InstanceActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	// 1. 调用 Executor 执行操作
	var status string
	var pid int
	var uptime int64
	var execErr error

	// 查找实例目录
	workDir, found := executor.FindInstanceDir(req.InstanceID)

	// 如果是 destroy 操作，即使目录找不到也视为成功
	if !found && req.Action != "destroy" {
		execErr = fmt.Errorf("instance dir not found for ID: %s", req.InstanceID)
	} else {
		switch req.Action {
		case "start":
			// StartProcess 返回的是结构体
			result := executor.StartProcess(workDir)
			status = result.Status
			pid = result.PID
			uptime = result.Uptime
			execErr = result.Error
		case "stop":
			// StopProcess 返回 (status, pid, err)
			status, pid, execErr = executor.StopProcess(workDir)
		case "destroy":
			// Destroy 直接调用 HandleAction
			execErr = executor.HandleAction(req)
			status = "destroyed"
			pid = 0
			uptime = 0
		default:
			execErr = fmt.Errorf("unsupported action: %s", req.Action)
		}
	}

	if execErr != nil {
		http.Error(w, execErr.Error(), 500)
		return
	}

	// 2. 向 Master 报告最新状态
	report := protocol.InstanceStatusReport{
		InstanceID: req.InstanceID,
		Status:     status,
		PID:        pid,
		Uptime:     uptime,
	}
	reportURL := fmt.Sprintf("%s/api/instance/status_report", masterBaseURL)
	reportBytes, _ := json.Marshal(report)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(reportURL, "application/json", bytes.NewBuffer(reportBytes))

	if err != nil {
		log.Printf("Failed to report status (Network Error): %v", err)
	} else {
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			// 读取 Master 返回的错误详情
			body, _ := io.ReadAll(resp.Body)
			log.Printf("Failed to report status (Master Rejected): Code=%d, Body=%s", resp.StatusCode, string(body))
		}
	}

	w.Write([]byte(`{"status":"ok"}`))
}

// handleExec 处理 CMD 命令
func handleExec(w http.ResponseWriter, r *http.Request) {
	var req protocol.CommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", req.Command)
	} else {
		cmd = exec.Command("sh", "-c", req.Command)
	}

	output, err := cmd.CombinedOutput()
	result := map[string]string{
		"output": string(output),
		"error":  "",
	}
	if err != nil {
		result["error"] = err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func handleGetLogFiles(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("instance_id")
	if id == "" {
		http.Error(w, "missing instance_id", 400)
		return
	}

	files, err := executor.GetLogFiles(id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	resp := protocol.LogFilesResp{
		InstanceID: id,
		Files:      files,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// [新增] 处理缓存清理请求
func handleCleanupCache(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}

	// 解析请求参数
	var req struct {
		Retain int `json:"retain"` // 保留数量，默认为 0 (全删)
	}

	// 允许 Body 为空，此时 Retain 默认为 0
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}
	}

	// 参数防御：如果不传或者传负数，默认可能比较危险
	// 这里我们约定：
	// n >= 0 : 保留 n 个
	// 如果用户真的想全删，传 0 即可

	result, err := executor.CleanupPackageCache(req.Retain)
	if err != nil {
		log.Printf("[Cleanup] Error: %v", err)
		http.Error(w, fmt.Sprintf("Cleanup failed: %v", err), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code": 0,
		"msg":  "success",
		"data": result,
	})
}
