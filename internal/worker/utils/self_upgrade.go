package utils

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// PerformSelfUpgrade 执行自升级流程
func PerformSelfUpgrade(downloadURL, checksum string) error {
	// 1. 获取当前可执行文件路径
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %v", err)
	}
	// 解析软链接（Linux下常见）
	currentExe, err = filepath.EvalSymlinks(currentExe)
	if err != nil {
		return fmt.Errorf("failed to eval symlinks: %v", err)
	}

	// [已修复] 删除了未使用的 workDir 变量

	newExe := currentExe + ".new" // 临时下载文件
	oldExe := currentExe + ".old" // 备份文件

	// 2. 下载新版本
	fmt.Printf("[Upgrade] Downloading from %s...\n", downloadURL)
	if err := downloadFile(newExe, downloadURL); err != nil {
		return fmt.Errorf("download failed: %v", err)
	}
	// 无论成功失败，最后清理掉 .new 文件（成功的会被重命名，失败的被清理）
	defer os.Remove(newExe)

	// 3. 校验 Checksum (SHA256)
	if err := verifyChecksum(newExe, checksum); err != nil {
		return fmt.Errorf("checksum mismatch: %v", err)
	}

	// 4. 赋予执行权限 (Linux/macOS)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(newExe, 0755); err != nil {
			return fmt.Errorf("chmod failed: %v", err)
		}
	}

	// 5. 文件替换 (Windows 兼容写法)
	// Windows 不允许覆盖正在运行的 exe，但允许重命名正在运行的 exe。
	// 策略：Current -> Old, New -> Current

	// 如果存在旧的备份，先删除
	os.Remove(oldExe)

	// A. 备份当前运行文件
	if err := os.Rename(currentExe, oldExe); err != nil {
		return fmt.Errorf("backup failed: %v", err)
	}

	// B. 将新文件上位
	if err := os.Rename(newExe, currentExe); err != nil {
		// 回滚
		os.Rename(oldExe, currentExe)
		return fmt.Errorf("replace executable failed: %v", err)
	}

	// 6. 重启进程
	fmt.Println("[Upgrade] File replaced. Restarting...")
	return restartProcess(currentExe)
}

// downloadFile 下载文件
func downloadFile(filepath string, url string) error {
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// 设置超时
	client := http.Client{
		Timeout: 300 * time.Second, // 5分钟超时
	}

	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	_, err = io.Copy(out, resp.Body)
	return err
}

// verifyChecksum 校验 SHA256
func verifyChecksum(path, expected string) error {
	if expected == "" {
		return nil // 不校验
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}

	actual := fmt.Sprintf("%x", h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("expected %s, got %s", expected, actual)
	}
	return nil
}

// restartProcess 重启自身
func restartProcess(exePath string) error {
	// 获取启动参数 (保持原样)
	args := os.Args[1:]

	cmd := exec.Command(exePath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// 保持环境变量
	cmd.Env = os.Environ()

	// 启动新进程
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start new process: %v", err)
	}

	// 退出当前进程
	fmt.Printf("[Upgrade] New process started (PID: %d). Exiting...\n", cmd.Process.Pid)
	os.Exit(0)
	return nil
}

// CalculateSelfHash 计算当前运行文件的 SHA256
func CalculateSelfHash() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	// 处理软链接
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return "", err
	}

	f, err := os.Open(exePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
