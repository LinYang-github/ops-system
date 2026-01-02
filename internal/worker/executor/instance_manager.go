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
	"gopkg.in/natefinch/lumberjack.v2"
)

// StartProcessResult 进程启动结果
type StartProcessResult struct {
	Status string
	PID    int
	Uptime int64
	Error  error
}

// InstanceDirInfo 实例目录信息
type InstanceDirInfo struct {
	InstanceID string
	WorkDir    string
}

// 辅助结构体，用于本地存储
type InstanceMeta struct {
	SystemID       string `json:"system_id"`
	NodeID         string `json:"node_id"` // 虽然 Worker 知道自己是谁，但存下来保持一致性
	ServiceName    string `json:"service_name"`
	ServiceVersion string `json:"service_version"`
}

// -------------------------------------------------------
// 目录查找与扫描逻辑 (挂载在 Manager 上)
// -------------------------------------------------------

// GetAllLocalInstances 获取本地所有实例 (包含标准实例和纳管实例)
func (m *Manager) GetAllLocalInstances() []InstanceDirInfo {
	var list []InstanceDirInfo
	if m.workDir == "" {
		return list
	}

	// 1. 扫描标准实例目录
	list = append(list, scanDir(m.workDir)...)

	// 2. 扫描纳管实例目录 (instances/external)
	extDir := filepath.Join(m.workDir, "external")
	list = append(list, scanDir(extDir)...)

	return list
}

// FindInstanceDir 根据 InstanceID 查找其实际工作目录
func (m *Manager) FindInstanceDir(instID string) (string, bool) {
	if m.workDir == "" {
		return "", false
	}

	// 辅助查找函数
	searchInRoot := func(rootDir string) (string, bool) {
		systems, err := os.ReadDir(rootDir)
		if err != nil {
			return "", false
		}
		for _, sys := range systems {
			if !sys.IsDir() {
				continue
			}
			// 跳过特殊目录
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
				// 匹配规则：目录名等于 ID，或者目录名以 "_ID" 结尾 (ServiceName_InstanceID)
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

	// 1. 先在根目录下找 (标准实例)
	if path, found := searchInRoot(m.workDir); found {
		return path, true
	}

	// 2. 去 external 目录下找 (纳管实例)
	extDir := filepath.Join(m.workDir, "external")
	if path, found := searchInRoot(extDir); found {
		return path, true
	}

	return "", false
}

// scanDir 扫描指定目录下的 System/Instance 结构 (私有辅助函数)
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
			// 尝试解析 ServiceName_InstanceID 格式
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

// -------------------------------------------------------
// 部署逻辑 (挂载在 Manager 上)
// -------------------------------------------------------

// DeployInstance 执行部署流程
func (m *Manager) DeployInstance(req protocol.DeployRequest) error {
	if m.workDir == "" {
		return fmt.Errorf("executor manager not initialized")
	}

	// 1. 确保包已缓存 (下载)
	cachedZipPath, err := m.ensurePackageCached(req.ServiceName, req.Version, req.DownloadURL)
	if err != nil {
		return fmt.Errorf("cache package failed: %v", err)
	}

	// 2. 准备实例目录
	dirName := fmt.Sprintf("%s_%s", req.ServiceName, req.InstanceID)
	workDir := filepath.Join(m.workDir, req.SystemName, dirName)

	log.Printf("[Deploy] Extracting %s -> %s", filepath.Base(cachedZipPath), workDir)

	// 3. 清理旧目录并重新解压
	os.RemoveAll(workDir)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return err
	}
	if err := unzip(cachedZipPath, workDir); err != nil {
		return fmt.Errorf("unzip failed: %v", err)
	}

	// [新增] 4.5 持久化元数据 (用于上报和自愈)
	meta := InstanceMeta{
		SystemID:       req.SystemName, // 注意：协议里 SystemName 其实传的是 SystemID
		ServiceName:    req.ServiceName,
		ServiceVersion: req.Version,
		// NodeID 可以动态获取，也可以存
	}
	if err := m.saveInstanceMeta(workDir, meta); err != nil {
		log.Printf("[Deploy] Warning: failed to save meta: %v", err)
	}

	// 4. 配置覆写 (Readiness Probe)
	if req.ReadinessType != "" {
		manifestPath := filepath.Join(workDir, "service.json")
		// 读取现有文件
		mf, err := readManifest(workDir)
		if err != nil {
			log.Printf("[Deploy] Warning: service.json read failed: %v", err)
		} else {
			// 覆盖字段
			mf.ReadinessType = req.ReadinessType
			mf.ReadinessTarget = req.ReadinessTarget
			mf.ReadinessTimeout = req.ReadinessTimeout

			// 写回文件
			file, err := os.Create(manifestPath)
			if err == nil {
				encoder := json.NewEncoder(file)
				encoder.SetIndent("", "  ")
				encoder.Encode(mf)
				file.Close()
				log.Printf("[Deploy] Updated readiness config in service.json")
			}
		}
	}

	return nil
}

// ensurePackageCached 下载并缓存服务包 (带文件锁)
func (m *Manager) ensurePackageCached(name, version, url string) (string, error) {
	fileName := fmt.Sprintf("%s_%s.zip", name, version)
	cachePath := filepath.Join(m.pkgCacheDir, fileName)

	// 1. 快速检查：如果已存在且非空，直接返回
	if info, err := os.Stat(cachePath); err == nil && info.Size() > 0 {
		return cachePath, nil
	}

	// 2. 加锁下载 (防止同一文件并发下载)
	// 使用 Manager 中的 sync.Map 存储锁
	muInterface, _ := m.downloadLocks.LoadOrStore(fileName, &sync.Mutex{})
	mu := muInterface.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	// 双重检查
	if info, err := os.Stat(cachePath); err == nil && info.Size() > 0 {
		return cachePath, nil
	}

	if err := os.MkdirAll(m.pkgCacheDir, 0755); err != nil {
		return "", fmt.Errorf("create cache dir failed: %v", err)
	}

	log.Printf("[Cache] Downloading to: %s", cachePath)
	tmpFile := cachePath + ".tmp"

	// 执行 HTTP 下载
	// 建议：此处可以复用 utils.GlobalClient，但为了解耦暂用 http.Get
	// 如果是大文件，建议加上 Context 和 Timeout 控制
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(url)
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

	// 流式写入
	_, err = io.Copy(out, resp.Body)
	out.Close()
	if err != nil {
		os.Remove(tmpFile)
		return "", err
	}

	// 原子重命名
	if err := os.Rename(tmpFile, cachePath); err != nil {
		return "", fmt.Errorf("rename failed: %v", err)
	}

	return cachePath, nil
}

// -------------------------------------------------------
// 进程控制逻辑 (挂载在 Manager 上)
// -------------------------------------------------------

// HandleAction 处理实例的操作请求
func (m *Manager) HandleAction(req protocol.InstanceActionRequest) error {
	workDir, found := m.FindInstanceDir(req.InstanceID)

	if !found {
		// 如果是销毁操作，且目录不存在，视为成功
		if req.Action == "destroy" {
			return nil
		}
		return fmt.Errorf("instance dir not found for ID: %s", req.InstanceID)
	}

	switch req.Action {
	case "start":
		res := m.StartProcess(workDir)
		return res.Error
	case "stop":
		_, _, err := m.StopProcess(workDir)
		return err
	case "destroy":
		m.StopProcess(workDir)
		return os.RemoveAll(workDir)
	}
	return nil
}

// StartProcess 启动进程 (包含就绪检测)
func (m *Manager) StartProcess(workDir string) StartProcessResult {
	// 1. 读取配置
	mf, err := readManifest(workDir)
	if err != nil {
		return StartProcessResult{Status: "error", Error: err}
	}

	// 2. 检查运行状态
	// [修改] 这里的 isRunning 现在需要更严格，如果残留 PID 文件但进程不在，应该视为未运行
	if m.strictIsRunning(workDir) { // 替换原来的 isRunning
		pid := getPID(workDir)
		return StartProcessResult{Status: "running", PID: pid, Uptime: time.Now().Unix(), Error: nil}
	} else {
		// [新增] 确保清理残留，防止 Start 失败后 PID 文件还在
		os.Remove(filepath.Join(workDir, "pid"))
	}

	// 3. 路径与环境准备
	execDir := workDir
	if mf.IsExternal {
		execDir = mf.ExternalWorkDir
	}

	cmdPath := mf.Entrypoint
	if !filepath.IsAbs(cmdPath) {
		cmdPath = filepath.Join(execDir, mf.Entrypoint)
	}

	absEntrypoint, err := resolveExecutable(cmdPath)
	if err != nil {
		return StartProcessResult{Status: "error", Error: fmt.Errorf("executable check failed: %v", err)}
	}

	log.Printf("[Start] Executing: %s (CWD: %s)", absEntrypoint, execDir)

	// 4. 构建命令
	cmd := exec.Command(absEntrypoint, mf.Args...)
	cmd.Dir = execDir
	cmd.Env = buildEnv(mf.Env)

	// 日志重定向 (使用 m.logRotateCfg)
	logPath := filepath.Join(workDir, "app.log")

	logger := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    m.logRotateCfg.MaxSize,
		MaxBackups: m.logRotateCfg.MaxBackups,
		MaxAge:     m.logRotateCfg.MaxAge,
		Compress:   m.logRotateCfg.Compress,
		LocalTime:  true,
	}

	// 如果配置了“每次启动生成新文件”
	if m.logRotateCfg.RotateOnStart {
		if _, err := os.Stat(logPath); err == nil {
			if err := logger.Rotate(); err != nil {
				fmt.Printf("[Warn] Failed to rotate log on start: %v\n", err)
			}
		}
	}

	cmd.Stdout = logger
	cmd.Stderr = logger

	// 【平台钩子】启动前准备 (Setsid / CreationFlags)
	// 注意：prepareProcess 是 process_unix.go/process_windows.go 中的包级函数
	prepareProcess(cmd)

	// 5. 执行启动
	if err := cmd.Start(); err != nil {
		return StartProcessResult{Status: "error", Error: fmt.Errorf("start failed: %v", err)}
	}

	// 【平台钩子】关联 Job Object (防止僵尸进程)
	// attachProcessToManager 是 process_windows.go 中的包级函数 (Linux 下为空操作)
	instID := filepath.Base(workDir)
	if err := attachProcessToManager(instID, cmd.Process.Pid); err != nil {
		log.Printf("[Warn] Attach to job failed: %v", err)
	}

	var targetPID int

	// 6. 获取目标 PID (Spawn vs Match)
	if mf.IsExternal && mf.PidStrategy == "match" {
		cmd.Wait() // 等待启动脚本结束

		// 轮询查找目标进程
		for i := 0; i < 5; i++ {
			time.Sleep(500 * time.Millisecond)
			pid, err := findProcessPID(mf.ProcessName, mf.ExternalWorkDir)
			if err == nil && pid > 0 {
				targetPID = pid
				break
			}
		}
		if targetPID == 0 {
			return StartProcessResult{Status: "error", Error: fmt.Errorf("process match failed for: %s", mf.ProcessName)}
		}
	} else {
		// Spawn 模式
		targetPID = cmd.Process.Pid
		go cmd.Wait() // 异步等待防止僵尸进程
	}

	// 写入 PID 文件
	os.WriteFile(filepath.Join(workDir, "pid"), []byte(strconv.Itoa(targetPID)), 0644)

	// 7. 就绪检测 (Readiness Probe)
	if mf.ReadinessType != "" && mf.ReadinessType != "none" {
		log.Printf("[Start] Waiting for readiness (%s -> %s)...", mf.ReadinessType, mf.ReadinessTarget)

		// waitReady 是 probe.go 中的包级函数
		if err := waitReady(mf.ReadinessType, mf.ReadinessTarget, mf.ReadinessTimeout); err != nil {
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
func (m *Manager) StopProcess(workDir string) (status string, pid int, err error) {
	mf, err := readManifest(workDir)

	// 1. 读取 PID
	pidPath := filepath.Join(workDir, "pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		// 没有 PID 文件，视为已停止
		return "stopped", 0, nil
	}
	targetPID, _ := strconv.Atoi(string(data))

	// 2. 优先执行自定义停止命令
	if mf != nil && mf.StopEntrypoint != "" {
		execDir := workDir
		if mf.IsExternal {
			execDir = mf.ExternalWorkDir
		}
		cmdPath := mf.StopEntrypoint
		if !filepath.IsAbs(cmdPath) {
			cmdPath = filepath.Join(execDir, cmdPath)
		}

		if absStop, resolveErr := resolveExecutable(cmdPath); resolveErr == nil {
			cmd := exec.Command(absStop, mf.StopArgs...)
			cmd.Dir = execDir
			cmd.Env = buildEnv(mf.Env)

			if out, runErr := cmd.CombinedOutput(); runErr != nil {
				log.Printf("[Stop] Custom stop command failed: %v, output: %s", runErr, string(out))
			} else {
				log.Printf("[Stop] Custom command executed successfully")
			}
		}
	}

	// 3. 强制终止逻辑 (平台特定)
	if targetPID > 0 {
		instID := filepath.Base(workDir)
		// killProcessTree 是 process_unix.go/process_windows.go 中的包级函数
		if err := killProcessTree(targetPID, instID); err != nil {
			if !isProcessNotFoundError(err) {
				return "error", targetPID, fmt.Errorf("kill process failed: %v", err)
			}
		}
	}

	// 4. 清理残留
	os.Remove(pidPath)
	return "stopped", 0, nil
}

// -------------------------------------------------------
// 无状态辅助函数 (Private Helpers)
// -------------------------------------------------------

func isProcessNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	// Unix / Windows / Go internal errors
	return strings.Contains(s, "no such process") ||
		strings.Contains(s, "element not found") ||
		strings.Contains(s, "process already finished")
}

func readManifest(workDir string) (*protocol.ServiceManifest, error) {
	f, err := os.Open(filepath.Join(workDir, "service.json"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var mf protocol.ServiceManifest
	if err := json.NewDecoder(f).Decode(&mf); err != nil {
		return nil, err
	}
	return &mf, nil
}

func resolveExecutable(path string) (string, error) {
	if runtime.GOOS == "windows" && filepath.Ext(path) == "" {
		if _, err := os.Stat(path + ".exe"); err == nil {
			path += ".exe"
		}
	} else {
		// Linux 下尝试赋予执行权限
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
	pidPath := filepath.Join(workDir, "pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return false
	}
	pid, _ := strconv.Atoi(string(data))
	if pid <= 0 {
		return false
	}

	// 使用 os.FindProcess + Signal(0) 检查进程是否存在
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	if runtime.GOOS == "windows" {
		// Windows 下 FindProcess 总是成功，需要额外检查
		// 这里简单假设如果没报错就是活着，实际上可能需要更复杂的 syscall 检查
		// 或者使用 gopsutil 检查
		exists, _ := process.PidExists(int32(pid))
		return exists
	} else {
		// Unix 发送 Signal 0 不会真的杀进程，只检查是否存在
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			return false
		}
	}
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
		// 防止 Zip Slip 漏洞
		fpath := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func findProcessPID(nameKeyword string, workDir string) (int, error) {
	procs, err := process.Processes()
	if err != nil {
		return 0, err
	}

	cleanWorkDir := filepath.Clean(workDir)

	for _, p := range procs {
		n, _ := p.Name()
		cmd, _ := p.Cmdline()

		// 模糊匹配进程名或命令行
		if strings.Contains(strings.ToLower(n), strings.ToLower(nameKeyword)) ||
			strings.Contains(strings.ToLower(cmd), strings.ToLower(nameKeyword)) {

			// 进一步匹配工作目录 (如果能获取到)
			if cwd, err := p.Cwd(); err == nil {
				if filepath.Clean(cwd) == cleanWorkDir {
					return int(p.Pid), nil
				}
			}
		}
	}
	return 0, fmt.Errorf("process not found")
}

// strictIsRunning 严格检查运行状态
func (m *Manager) strictIsRunning(workDir string) bool {
	pidPath := filepath.Join(workDir, "pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return false
	}
	pid, _ := strconv.Atoi(string(data))

	proc, err := process.NewProcess(int32(pid))
	if err != nil {
		return false
	}

	// 复用 validator 的逻辑 (如果找不到 exeName 传空，只校验 CWD)
	return m.validateProcessIdentity(proc, workDir, "")
}

// [新增] 辅助方法：保存元数据
func (m *Manager) saveInstanceMeta(workDir string, meta InstanceMeta) error {
	f, err := os.Create(filepath.Join(workDir, ".meta"))
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(meta)
}
