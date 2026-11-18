//go:build windows

package agent

import (
	"os/exec"
	"strconv"
	"syscall"
)

// IsolateProcess makes cancellation terminate the command and its descendants.
// The command must have been created with exec.CommandContext.
func IsolateProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200, HideWindow: true}
	cmd.Cancel = func() error { return killProcessTree(cmd) }
}
func killProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	killer := exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
	killer.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := killer.Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
