//go:build !windows

package review

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This helper models npm's Node wrapper and native CLI without depending on Node
// or a user's installation, credentials, or authorization state.
func TestZsxqCLIProcessHelper(t *testing.T) {
	switch os.Getenv("ZSXQ_TEST_PROCESS_ROLE") {
	case "wrapper":
		child := exec.Command(os.Args[0], "-test.run=^TestZsxqCLIProcessHelper$")
		child.Env = append(os.Environ(), "ZSXQ_TEST_PROCESS_ROLE=child")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Run(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	case "child":
		if err := os.WriteFile(os.Getenv("ZSXQ_TEST_PID_FILE"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			os.Exit(2)
		}
		for {
			fmt.Fprintln(os.Stdout, "pending")
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func TestZsxqCLICancellationStopsWrapperAndNativeChild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("ZSXQ_TEST_PROCESS_ROLE", "wrapper")
	t.Setenv("ZSXQ_TEST_PID_FILE", pidFile)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := runZsxqCLI(ctx, os.Args[0], "-test.run=^TestZsxqCLIProcessHelper$")
		done <- err
	}()
	t.Cleanup(cancel)
	var childPID int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil {
			childPID, err = strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childPID == 0 {
		t.Fatal("native child did not start")
	}
	t.Cleanup(func() { _ = syscall.Kill(childPID, syscall.SIGKILL) })
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation cause was not preserved: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CLI did not return after cancellation")
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(childPID, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("native CLI child survived cancellation")
}

func TestZsxqCLIPreservesDeadlineExceeded(t *testing.T) {
	t.Setenv("ZSXQ_TEST_PROCESS_ROLE", "child")
	t.Setenv("ZSXQ_TEST_PID_FILE", filepath.Join(t.TempDir(), "child.pid"))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := runZsxqCLI(ctx, os.Args[0], "-test.run=^TestZsxqCLIProcessHelper$")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline cause was not preserved: %v", err)
	}
}
