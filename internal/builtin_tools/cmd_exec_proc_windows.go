//go:build windows

package builtin_tools

import (
	"os/exec"
	"strconv"
	"syscall"
)

// setProcGroup 让子进程成为新进程组的根，便于后续整组清理。
func setProcGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= syscall.CREATE_NEW_PROCESS_GROUP
}

// killGroup 用 taskkill /T 杀掉整棵进程树（Windows 无 POSIX 进程组语义）。
// killSignal 档带 /F 强制终止。
func killGroup(cmd *exec.Cmd, sig procSignal) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	args := []string{"/T", "/PID", pid}
	if sig == killSignal {
		args = append([]string{"/F"}, args...)
	}
	_ = exec.Command("taskkill", args...).Run()
}
