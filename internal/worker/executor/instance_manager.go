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

// InstanceDirInfo 辅助结构
type InstanceDirInfo struct {
	InstanceID string
	WorkDir    string
}

// scanInstancesInDir 扫描逻辑
func scanInstancesInDir(parentDir string) []InstanceDirInfo {
	var list []InstanceDirInfo
	sysEntries, err := os.ReadDir(parentDir)
	if err != nil {
		return list
	}

	for _, sys := range sysEntries {
		if !sys.IsDir() {
			continue
		}
		sys_name := sys.Name()
		if sys_name == "pkg_cache" || sys_name == "external" {
			continue
		}
		sysPath := filepath.Join(parentDir, sys.Name())

		instEntries, err := os.ReadDir(sysPath)
		if err != nil {
			continue
		}

		for _, inst := range instEntries {
			if !inst.IsDir() {
				continue
			}
			name := inst.Name()
			lastUnderscore := -1
			for i := len(name) - 1; i >= 0; i-- {
				if name[i] == '_' {
					lastUnderscore = i
					break
				}
			}
			if lastUnderscore > 0 && lastUnderscore < len(name)-1 {
				instID := name[lastUnderscore+1:]
				list = append(list, InstanceDirInfo{
					InstanceID: instID,
					WorkDir:    filepath.Join(sysPath, name),
				})
			}
		}
	}
	return list
}

func scanDir(parentDir string) []InstanceDirInfo {
	// 复用 scanInstancesInDir 的逻辑即可，或者保留之前的 scanDir
	return scanInstancesInDir(parentDir)
}

// GetAllLocalInstances 扫描本地所有实例
func GetAllLocalInstances() []InstanceDirInfo {
	var list []InstanceDirInfo
	if baseWorkDir == "" {
		return list
	}
	list = append(list, scanInstancesInDir(baseWorkDir)...)
	extDir := filepath.Join(baseWorkDir, "external")
	list = append(list, scanInstancesInDir(extDir)...)
	return list
}

// DeployInstance 部署实例
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

// HandleAction 处理动作
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

// StartProcess 启动
func StartProcess(workDir string) StartProcessResult {
	m, err := readManifest(workDir)
	if err != nil {
		return StartProcessResult{Status: "error", Error: err}
	}

	if isRunning(workDir) {
		pid := getPID(workDir)
		return StartProcessResult{Status: "running", PID: pid, Uptime: time.Now().Unix(), Error: nil}
	}

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

	cmd := exec.Command(absEntrypoint, m.Args...)
	cmd.Dir = execDir
	cmd.Env = buildEnv(m.Env)

	logFile, _ := os.Create(filepath.Join(workDir, "app.log"))
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// ==========================================
	// 【关键修改】设置进程属性，使其脱离父进程
	// 具体实现由 process_windows.go 或 process_unix.go 提供
	// ==========================================
	setProcessAttributes(cmd)

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return StartProcessResult{Status: "error", Error: fmt.Errorf("start failed: %v", err)}
	}

	var targetPID int

	if m.IsExternal && m.PidStrategy == "match" {
		cmd.Wait() // 等待脚本退出
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
		targetPID = cmd.Process.Pid
		// 异步等待，防止僵尸进程，但不阻塞 Worker
		go cmd.Wait()
	}

	os.WriteFile(filepath.Join(workDir, "pid"), []byte(strconv.Itoa(targetPID)), 0644)
	return StartProcessResult{Status: "running", PID: targetPID, Uptime: time.Now().Unix()}
}

// StopProcess 停止
func StopProcess(workDir string) (status string, pid int, err error) {
	m, err := readManifest(workDir)
	if err == nil && m.StopEntrypoint != "" {
		execDir := workDir
		if m.IsExternal {
			execDir = m.ExternalWorkDir
		}
		cmdPath := m.StopEntrypoint
		if !filepath.IsAbs(cmdPath) {
			cmdPath = filepath.Join(execDir, cmdPath)
		}
		absStop, _ := resolveExecutable(cmdPath)
		if absStop != "" {
			cmd := exec.Command(absStop, m.StopArgs...)
			cmd.Dir = execDir
			cmd.Env = buildEnv(m.Env)
			cmd.Run()
		}
	}

	pidPath := filepath.Join(workDir, "pid")
	data, _ := os.ReadFile(pidPath)
	targetPID, _ := strconv.Atoi(string(data))

	if targetPID > 0 {
		if proc, err := os.FindProcess(targetPID); err == nil {
			proc.Kill()
		}
	}
	os.Remove(pidPath)
	return "stopped", 0, nil
}

// FindInstanceDir 查找实例目录
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

// 辅助函数
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
		// Unix: signal 0
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			return false
		}
	}
	// Windows: FindProcess always succeeds, needs extra check or rely on pidfile
	// Simple check:
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
		if strings.Contains(strings.ToLower(n), strings.ToLower(nameKeyword)) ||
			strings.Contains(strings.ToLower(cmd), strings.ToLower(nameKeyword)) {
			if cwd, err := p.Cwd(); err == nil && filepath.Clean(cwd) == workDir {
				return int(p.Pid), nil
			}
		}
	}
	return 0, fmt.Errorf("not found")
}
