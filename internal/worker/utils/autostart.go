package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const ServiceName = "ops-worker"

// HandleAutoStart 处理自启逻辑
// enable: true=开启, false=关闭
// masterAddr, port, workDir: 当前的启动参数，用于写入配置文件
func HandleAutoStart(enable bool, masterAddr string, port int, workDir string) error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	exePath, _ = filepath.Abs(exePath)

	// 拼接启动参数
	// 注意：自启时不能包含 -autostart 参数，否则会死循环
	args := []string{
		fmt.Sprintf("-master=%s", masterAddr),
		fmt.Sprintf("-port=%d", port),
		fmt.Sprintf("-work_dir=%s", workDir),
	}

	if enable {
		fmt.Printf("正在配置开机自启...\n 执行文件: %s\n 参数: %s\n", exePath, strings.Join(args, " "))
		return enableAutoStart(exePath, args)
	} else {
		fmt.Printf("正在取消开机自启...\n")
		return disableAutoStart()
	}
}
