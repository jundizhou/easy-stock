//go:build !windows

package agent

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// IsolateProcess makes cancellation terminate the command and its descendants.
// The command must have been created with exec.CommandContext.
func IsolateProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killProcessTree(cmd) }
}
func killProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// Codex MCP workers may establish separate process groups. Include those
	// descendants while the parent still owns them; never scan by executable name.
	output, _ := exec.Command("ps", "-axo", "pid=,ppid=").Output()
	children := map[int][]int{}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pid, _ := strconv.Atoi(fields[0])
		parent, _ := strconv.Atoi(fields[1])
		children[parent] = append(children[parent], pid)
	}
	var stopChildren func(int)
	stopChildren = func(parent int) {
		for _, pid := range children[parent] {
			stopChildren(pid)
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	stopChildren(cmd.Process.Pid)
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if err == syscall.ESRCH {
		return os.ErrProcessDone
	}
	return err
}
