package executor

import (
	"path/filepath"
	"strings"

	"github.com/shirou/gopsutil/v3/process"
)

// validateProcessIdentity 验证 PID 对应的进程是否真的是我们的实例
// 防止 PID 复用 (PID Reuse) 导致的状态误判
func (m *Manager) validateProcessIdentity(proc *process.Process, workDir string, exeName string) bool {
	// 1. 尝试获取进程的工作目录 (CWD)
	// 这是最准确的判断依据，因为每个实例都有独立的目录
	cwd, err := proc.Cwd()
	if err == nil {
		// 标准化路径 (处理 Windows/Linux 斜杠差异)
		cleanProcCwd := filepath.Clean(strings.ToLower(cwd))
		cleanWorkDir := filepath.Clean(strings.ToLower(workDir))

		// 如果进程的 CWD 包含实例目录，视为匹配
		// 使用 HasPrefix 是为了兼容子目录运行的情况
		if strings.HasPrefix(cleanProcCwd, cleanWorkDir) {
			return true
		}

		// 特殊情况：如果是纳管的外部进程，CWD 可能完全不同
		// 此时如果有明确的 ExternalWorkDir，则匹配它
		// 这里简化逻辑：如果不匹配，继续尝试匹配 Exe Name
	}

	// 2. 如果 CWD 无法获取 (权限问题) 或不匹配，尝试匹配进程名
	// 这作为兜底策略
	name, err := proc.Name()
	if err == nil && exeName != "" {
		if strings.Contains(strings.ToLower(name), strings.ToLower(exeName)) {
			return true
		}
	}

	// 3. 尝试匹配命令行参数 (最宽松的匹配)
	cmdline, err := proc.Cmdline()
	if err == nil && cmdline != "" {
		// 如果命令行包含工作目录路径，通常也是我们的进程
		if strings.Contains(strings.ToLower(cmdline), strings.ToLower(workDir)) {
			return true
		}
	}

	// 如果所有特征都对不上，说明这个 PID 已经被系统分配给了别的进程
	return false
}
