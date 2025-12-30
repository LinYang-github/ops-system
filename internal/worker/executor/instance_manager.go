package executor

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"ops-system/pkg/protocol"

	"github.com/shirou/gopsutil/v3/process"
)

// 全局变量
var (
	baseWorkDir string // .../instances
	pkgCacheDir string // .../instances/pkg_cache
)

// 下载锁
var downloadLocks sync.Map

// Init 初始化基础目录
func Init(dir string) {
	baseWorkDir = dir
	pkgCacheDir = filepath.Join(baseWorkDir, "pkg_cache")

	os.MkdirAll(baseWorkDir, 0755)
	os.MkdirAll(pkgCacheDir, 0755)

	log.Printf("Executor Init.")
	log.Printf(" > Work Dir:  %s", baseWorkDir)
	log.Printf(" > Cache Dir: %s", pkgCacheDir)
}

type StartProcessResult struct {
	Status string
	PID    int
	Uptime int64
	Error  error
}

type InstanceDirInfo struct {
	InstanceID string
	WorkDir    string
}

// -------------------------------------------------------
// 扫描与目录查找逻辑
// -------------------------------------------------------

func scanDir(parentDir string) []InstanceDirInfo {
	var list []InstanceDirInfo
	sysEntries, err := os.ReadDir(parentDir)
	if err != nil {
		return list
	}

	for _, sys := range sysEntries {
		if !sys.IsDir() {
			continue
		}
		sysName := sys.Name()
		if sysName == "pkg_cache" || sysName == "external" {
			continue
		}
		sysPath := filepath.Join(parentDir, sysName)

		instEntries, err := os.ReadDir(sysPath)
		if err != nil {
			continue
		}

		for _, inst := range instEntries {
			if !inst.IsDir() {
				continue
			}
			instName := inst.Name()
			instID := instName
			lastIdx := strings.LastIndex(instName, "_")
			if lastIdx != -1 && lastIdx < len(instName)-1 {
				instID = instName[lastIdx+1:]
			}

			list = append(list, InstanceDirInfo{
				InstanceID: instID,
				WorkDir:    filepath.Join(sysPath, instName),
			})
		}
	}
	return list
}

func GetAllLocalInstances() []InstanceDirInfo {
	var list []InstanceDirInfo
	if baseWorkDir == "" {
		return list
	}
	list = append(list, scanDir(baseWorkDir)...)
	extDir := filepath.Join(baseWorkDir, "external")
	list = append(list, scanDir(extDir)...)
	return list
}

func FindInstanceDir(instID string) (string, bool) {
	if baseWorkDir == "" {
		return "", false
	}
	searchInRoot := func(rootDir string) (string, bool) {
		systems, err := os.ReadDir(rootDir)
		if err != nil {
			return "", false
		}
		for _, sys := range systems {
			if !sys.IsDir() {
				continue
			}
			if sys.Name() == "pkg_cache" || sys.Name() == "external" {
				continue
			}
			sysPath := filepath.Join(rootDir, sys.Name())
			insts, err := os.ReadDir(sysPath)
			if err != nil {
				continue
			}
			for _, inst := range insts {
				if !inst.IsDir() {
					continue
				}
				name := inst.Name()
				if name == instID {
					return filepath.Join(sysPath, name), true
				}
				if strings.HasSuffix(name, "_"+instID) {
					return filepath.Join(sysPath, name), true
				}
			}
		}
		return "", false
	}
	if path, found := searchInRoot(baseWorkDir); found {
		return path, true
	}
	extDir := filepath.Join(baseWorkDir, "external")
	if path, found := searchInRoot(extDir); found {
		return path, true
	}
	return "", false
}

// -------------------------------------------------------
// 部署逻辑
// -------------------------------------------------------

func DeployInstance(req protocol.DeployRequest) error {
	if baseWorkDir == "" {
		return fmt.Errorf("executor not initialized")
	}
	cachedZipPath, err := ensurePackageCached(req.ServiceName, req.Version, req.DownloadURL)
	if err != nil {
		return fmt.Errorf("cache package failed: %v", err)
	}
	dirName := fmt.Sprintf("%s_%s", req.ServiceName, req.InstanceID)
	workDir := filepath.Join(baseWorkDir, req.SystemName, dirName)

	log.Printf("[Deploy] Extracting %s -> %s", filepath.Base(cachedZipPath), workDir)
	os.RemoveAll(workDir)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return err
	}
	if err := unzip(cachedZipPath, workDir); err != nil {
		return fmt.Errorf("unzip failed: %v", err)
	}

	// 【新增】配置覆写逻辑
	// 如果 Master 下发了 Readiness 配置，我们需要更新解压出来的 service.json
	if req.ReadinessType != "" {
		manifestPath := filepath.Join(workDir, "service.json")

		// 1. 读取现有文件
		m, err := readManifest(workDir)
		if err != nil {
			log.Printf("[Deploy] Warning: service.json read failed: %v", err)
		} else {
			// 2. 覆盖字段
			m.ReadinessType = req.ReadinessType
			m.ReadinessTarget = req.ReadinessTarget
			m.ReadinessTimeout = req.ReadinessTimeout

			// 3. 写回文件
			file, err := os.Create(manifestPath)
			if err == nil {
				encoder := json.NewEncoder(file)
				encoder.SetIndent("", "  ")
				encoder.Encode(m)
				file.Close()
				log.Printf("[Deploy] Updated readiness config in service.json")
			}
		}
	}

	return nil
}

func ensurePackageCached(name, version, url string) (string, error) {
	fileName := fmt.Sprintf("%s_%s.zip", name, version)
	cachePath := filepath.Join(pkgCacheDir, fileName)
	if info, err := os.Stat(cachePath); err == nil && info.Size() > 0 {
		return cachePath, nil
	}
	muInterface, _ := downloadLocks.LoadOrStore(fileName, &sync.Mutex{})
	mu := muInterface.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	if info, err := os.Stat(cachePath); err == nil && info.Size() > 0 {
		return cachePath, nil
	}
	if err := os.MkdirAll(pkgCacheDir, 0755); err != nil {
		return "", fmt.Errorf("create cache dir failed: %v", err)
	}
	log.Printf("[Cache] Downloading to: %s", cachePath)
	tmpFile := cachePath + ".tmp"
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("http error: %d", resp.StatusCode)
	}
	out, err := os.Create(tmpFile)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(out, resp.Body)
	out.Close()
	if err != nil {
		os.Remove(tmpFile)
		return "", err
	}
	if err := os.Rename(tmpFile, cachePath); err != nil {
		return "", fmt.Errorf("rename failed: %v", err)
	}
	return cachePath, nil
}

// -------------------------------------------------------
// 进程控制逻辑
// -------------------------------------------------------

func HandleAction(req protocol.InstanceActionRequest) error {
	workDir, found := FindInstanceDir(req.InstanceID)
	if !found {
		if req.Action == "destroy" {
			return nil
		}
		return fmt.Errorf("instance dir not found for ID: %s", req.InstanceID)
	}
	switch req.Action {
	case "start":
		res := StartProcess(workDir)
		return res.Error
	case "stop":
		_, _, err := StopProcess(workDir)
		return err
	case "destroy":
		StopProcess(workDir)
		return os.RemoveAll(workDir)
	}
	return nil
}

// StartProcess 启动进程 (包含就绪检测)
func StartProcess(workDir string) StartProcessResult {
	// 1. 读取配置
	m, err := readManifest(workDir)
	if err != nil {
		return StartProcessResult{Status: "error", Error: err}
	}

	// 2. 检查运行状态 (防止重复启动)
	if isRunning(workDir) {
		pid := getPID(workDir)
		return StartProcessResult{Status: "running", PID: pid, Uptime: time.Now().Unix(), Error: nil}
	}

	// 3. 路径与环境准备
	execDir := workDir
	if m.IsExternal {
		execDir = m.ExternalWorkDir
	}

	cmdPath := m.Entrypoint
	if !filepath.IsAbs(cmdPath) {
		cmdPath = filepath.Join(execDir, m.Entrypoint)
	}

	absEntrypoint, err := resolveExecutable(cmdPath)
	if err != nil {
		return StartProcessResult{Status: "error", Error: fmt.Errorf("executable check failed: %v", err)}
	}

	log.Printf("[Start] Executing: %s (CWD: %s)", absEntrypoint, execDir)

	// 4. 构建命令
	cmd := exec.Command(absEntrypoint, m.Args...)
	cmd.Dir = execDir
	cmd.Env = buildEnv(m.Env)

	// 日志重定向
	logFile, _ := os.Create(filepath.Join(workDir, "app.log"))
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// 【平台钩子】启动前准备 (Setsid / CreationFlags)
	prepareProcess(cmd)

	// 5. 执行启动
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return StartProcessResult{Status: "error", Error: fmt.Errorf("start failed: %v", err)}
	}

	// 【平台钩子】关联 Job Object
	instID := filepath.Base(workDir)
	if err := attachProcessToManager(instID, cmd.Process.Pid); err != nil {
		log.Printf("[Warn] Attach to job failed: %v", err)
	}

	var targetPID int

	// 6. 获取目标 PID (Spawn vs Match)
	if m.IsExternal && m.PidStrategy == "match" {
		cmd.Wait() // 等待启动脚本结束

		// 轮询查找目标进程
		for i := 0; i < 5; i++ {
			time.Sleep(500 * time.Millisecond)
			pid, err := findProcessPID(m.ProcessName, m.ExternalWorkDir)
			if err == nil && pid > 0 {
				targetPID = pid
				break
			}
		}
		if targetPID == 0 {
			return StartProcessResult{Status: "error", Error: fmt.Errorf("process match failed for: %s", m.ProcessName)}
		}
	} else {
		// Spawn 模式
		targetPID = cmd.Process.Pid
		go cmd.Wait() // 异步等待防止僵尸进程
	}

	// 写入 PID 文件
	os.WriteFile(filepath.Join(workDir, "pid"), []byte(strconv.Itoa(targetPID)), 0644)

	// 7. 【核心新增】就绪检测 (Readiness Probe)
	// 进程虽然起来了，但服务可能还没准备好 (e.g. Nacos 初始化中)
	if m.ReadinessType != "" && m.ReadinessType != "none" {
		log.Printf("[Start] Waiting for readiness (%s -> %s)...", m.ReadinessType, m.ReadinessTarget)

		if err := waitReady(m.ReadinessType, m.ReadinessTarget, m.ReadinessTimeout); err != nil {
			// 检测失败，视为启动失败
			// 策略选择：你可以选择这里 kill 进程，或者保留进程但返回 error 让用户处理
			// 这里我们返回 error，前端会显示红色状态，用户可以查看日志判断
			return StartProcessResult{
				Status: "error",
				PID:    targetPID,
				Uptime: time.Now().Unix(),
				Error:  fmt.Errorf("readiness probe failed: %v", err),
			}
		}
		log.Printf("[Start] Service ready.")
	}

	return StartProcessResult{Status: "running", PID: targetPID, Uptime: time.Now().Unix()}
}

// StopProcess 停止进程
func StopProcess(workDir string) (status string, pid int, err error) {
	m, err := readManifest(workDir)

	// 1. 读取 PID
	pidPath := filepath.Join(workDir, "pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		// 没有 PID 文件，视为已停止
		return "stopped", 0, nil
	}
	targetPID, _ := strconv.Atoi(string(data))

	// 2. 优先执行自定义停止命令
	if m != nil && m.StopEntrypoint != "" {
		execDir := workDir
		if m.IsExternal {
			execDir = m.ExternalWorkDir
		}
		cmdPath := m.StopEntrypoint
		if !filepath.IsAbs(cmdPath) {
			cmdPath = filepath.Join(execDir, cmdPath)
		}

		// 尝试解析并执行
		if absStop, resolveErr := resolveExecutable(cmdPath); resolveErr == nil {
			cmd := exec.Command(absStop, m.StopArgs...)
			cmd.Dir = execDir
			cmd.Env = buildEnv(m.Env)
			// 执行停止脚本，如果有错只打印日志，继续执行强制杀进程作为保底
			if out, runErr := cmd.CombinedOutput(); runErr != nil {
				log.Printf("[Stop] Custom stop command failed: %v, output: %s", runErr, string(out))
			} else {
				log.Printf("[Stop] Custom command executed successfully")
			}
		}
	}

	// 3. 强制终止逻辑 (平台特定)
	// 【关键修改】如果 Kill 失败，必须返回错误，而不是吞掉
	if targetPID > 0 {
		// 获取实例 ID 用于 Windows Job Object，Linux 下其实用不到
		instID := filepath.Base(workDir)

		if err := killProcessTree(targetPID, instID); err != nil {
			// 如果是 "进程不存在" 错误，我们认为这是成功的
			if !isProcessNotFoundError(err) {
				return "error", targetPID, fmt.Errorf("kill process failed: %v", err)
			}
		}
	}

	// 4. 清理残留
	os.Remove(pidPath)
	return "stopped", 0, nil
}

// 辅助函数：判断错误是否为“进程不存在”
func isProcessNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	// Unix: ESRCH (no such process)
	if strings.Contains(err.Error(), "no such process") {
		return true
	}
	// Windows: specific error text
	if strings.Contains(err.Error(), "element not found") { // Win32 ERROR_NOT_FOUND
		return true
	}
	// Go os.FindProcess 某些情况的返回
	if strings.Contains(err.Error(), "process already finished") {
		return true
	}
	return false
}

func readManifest(workDir string) (*protocol.ServiceManifest, error) {
	f, err := os.Open(filepath.Join(workDir, "service.json"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var m protocol.ServiceManifest
	json.NewDecoder(f).Decode(&m)
	return &m, nil
}
func resolveExecutable(path string) (string, error) {
	if runtime.GOOS == "windows" && filepath.Ext(path) == "" {
		if _, err := os.Stat(path + ".exe"); err == nil {
			path += ".exe"
		}
	} else {
		os.Chmod(path, 0755)
	}
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return path, nil
}
func buildEnv(custom map[string]string) []string {
	env := os.Environ()
	for k, v := range custom {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	return env
}
func isRunning(workDir string) bool {
	// ... (代码同前，略)
	pidPath := filepath.Join(workDir, "pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return false
	}
	pid, _ := strconv.Atoi(string(data))
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if runtime.GOOS != "windows" {
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			return false
		}
	}
	_ = proc
	return true
}
func getPID(workDir string) int {
	data, _ := os.ReadFile(filepath.Join(workDir, "pid"))
	pid, _ := strconv.Atoi(string(data))
	return pid
}
func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)
		if !filepath.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal path")
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}
		os.MkdirAll(filepath.Dir(fpath), os.ModePerm)
		out, _ := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		rc, _ := f.Open()
		io.Copy(out, rc)
		out.Close()
		rc.Close()
	}
	return nil
}
func findProcessPID(nameKeyword string, workDir string) (int, error) {
	procs, err := process.Processes()
	if err != nil {
		return 0, err
	}
	workDir = filepath.Clean(workDir)
	for _, p := range procs {
		n, _ := p.Name()
		cmd, _ := p.Cmdline()
		if strings.Contains(strings.ToLower(n), strings.ToLower(nameKeyword)) || strings.Contains(strings.ToLower(cmd), strings.ToLower(nameKeyword)) {
			if cwd, err := p.Cwd(); err == nil && filepath.Clean(cwd) == workDir {
				return int(p.Pid), nil
			}
		}
	}
	return 0, fmt.Errorf("not found")
}
