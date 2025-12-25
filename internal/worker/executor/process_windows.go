package executor

import (
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// prepareProcess 设置 Windows 进程标志
func prepareProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// attachProcessToManager 将进程加入 Job Object
func attachProcessToManager(uniqueID string, pid int) error {
	jobName, err := syscall.UTF16PtrFromString("Global\\OpsJob_" + uniqueID)
	if err != nil {
		return err
	}

	hJob, err := windows.OpenJobObject(windows.JOB_OBJECT_ASSIGN_PROCESS|windows.JOB_OBJECT_TERMINATE, false, jobName)
	if err != nil {
		hJob, err = windows.CreateJobObject(nil, jobName)
		if err != nil {
			return fmt.Errorf("create job failed: %v", err)
		}
	}
	defer windows.CloseHandle(windows.Handle(hJob))

	hProcess, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("open process failed: %v", err)
	}
	defer windows.CloseHandle(hProcess)

	err = windows.AssignProcessToJobObject(windows.Handle(hJob), hProcess)
	if err != nil {
		return fmt.Errorf("assign failed: %v", err)
	}

	return nil
}

// killProcessTree 通过 Job Object 杀进程树
// Windows 下主要依赖 uniqueID 找到 Job，pid 参数仅作备用
func killProcessTree(pid int, uniqueID string) error {
	jobName, err := syscall.UTF16PtrFromString("Global\\OpsJob_" + uniqueID)
	if err != nil {
		return err
	}

	hJob, err := windows.OpenJobObject(windows.JOB_OBJECT_TERMINATE, false, jobName)
	if err != nil {
		// Job 不存在，说明进程可能早就没了，返回 nil 视为成功
		return nil
	}
	defer windows.CloseHandle(windows.Handle(hJob))

	// 终止 Job
	if err := windows.TerminateJobObject(windows.Handle(hJob), 1); err != nil {
		return err
	}
	return nil
}
