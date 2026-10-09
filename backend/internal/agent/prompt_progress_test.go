package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func progressFixture(t *testing.T, events string) *HermesRuntime {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX gateway fixture")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte("MODEL_API_KEY=test-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(root, "gateway.sh")
	script := `#!/bin/sh
printf '%s\n' '{"method":"gateway.ready"}'
IFS= read -r create
printf '%s\n' '{"id":"1","result":{"session_id":"fixture"}}'
IFS= read -r submit
` + events
	if err := os.WriteFile(launcher, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	r := NewHermesRuntime(HermesConfig{Home: home, WorkDir: root, PythonPath: launcher})
	r.configured = true
	r.reasoningResolved = map[string]ReasoningCapability{capabilityKey("", "") + "|": reasoningCapability("unknown", "", "fixture", "default", "default")}
	return r
}

func TestPromptProgressKeepsActiveReasoningAlive(t *testing.T) {
	r := progressFixture(t, `i=0
while [ "$i" -lt 8 ]; do
printf '%s\n' '{"method":"reasoning.delta","params":{"text":"thinking"}}'
sleep 0.07
i=$((i+1))
done
printf '%s\n' '{"method":"message.complete","params":{"content":"{\"ok\":true}"}}'
`)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := r.PromptWithOptions(ctx, "test", PromptOptions{Sandbox: true, FirstResponseTimeout: time.Second, IdleTimeout: 300 * time.Millisecond})
	if err != nil || result.Content != `{"ok":true}` || result.Progress.ReasoningBytes != 64 || result.Progress.ElapsedMS < 300 {
		t.Fatalf("active generation interrupted or metrics lost: %+v %v", result, err)
	}
}

func TestPromptProgressWaitingHintsDoNotResetFirstResponse(t *testing.T) {
	r := progressFixture(t, `i=0
while [ "$i" -lt 20 ]; do
printf '%s\n' '{"method":"thinking.delta","params":{"text":"waiting"}}'
sleep 0.03
i=$((i+1))
done
`)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := r.PromptWithOptions(ctx, "test", PromptOptions{FirstResponseTimeout: 150 * time.Millisecond, IdleTimeout: time.Second})
	var timeout *PromptTimeoutError
	if !errors.As(err, &timeout) || !errors.Is(err, context.DeadlineExceeded) || timeout.Kind != "first_response" || result.Progress.ReasoningBytes != 0 {
		t.Fatalf("waiting hints kept a stalled call alive: %+v %v", result, err)
	}
}

func TestPromptProgressIdleTimeoutPreservesPartialOutput(t *testing.T) {
	r := progressFixture(t, `printf '%s\n' '{"method":"reasoning.delta","params":{"text":"reasoning"}}'
printf '%s\n' '{"method":"message.delta","params":{"text":"{\"part\":"}}'
sleep 0.6
`)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := r.PromptWithOptions(ctx, "test", PromptOptions{FirstResponseTimeout: time.Second, IdleTimeout: 150 * time.Millisecond})
	var timeout *PromptTimeoutError
	if !errors.As(err, &timeout) || timeout.Kind != "idle" || result.Content != `{"part":` || result.Progress.TextBytes == 0 || result.Progress.ReasoningBytes == 0 || !strings.Contains(err.Error(), "已接收正文") {
		t.Fatalf("partial output discarded: %+v %v", result, err)
	}
}

func TestPromptProgressTotalBudgetStillStopsActiveGeneration(t *testing.T) {
	r := progressFixture(t, `while true; do
printf '%s\n' '{"method":"reasoning.delta","params":{"text":"thinking"}}'
sleep 0.03
done
`)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := r.PromptWithOptions(ctx, "test", PromptOptions{Sandbox: true, FirstResponseTimeout: 2 * time.Second, IdleTimeout: 2 * time.Second})
	var timeout *PromptTimeoutError
	if !errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) || result.Progress.ReasoningBytes == 0 {
		t.Fatalf("total deadline lost: %+v %v", result, err)
	}
}

func TestPromptProgressStopsRepeatedRuntimeRetriesAndPreservesDraft(t *testing.T) {
	r := progressFixture(t, `printf '%s\n' '{"method":"message.delta","params":{"text":"draft"}}'
printf '%s\n' '{"method":"status.update","params":{"kind":"retry","text":"retry 1"}}'
printf '%s\n' '{"method":"reasoning.delta","params":{"text":"thinking"}}'
printf '%s\n' '{"method":"status.update","params":{"kind":"retry","text":"retry 2"}}'
printf '%s\n' '{"method":"message.complete","params":{"content":"must not succeed"}}'
`)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := r.PromptWithOptions(ctx, "test", PromptOptions{Sandbox: true, FirstResponseTimeout: time.Second, IdleTimeout: time.Second, MaxAttempts: 2})
	var limit *PromptRetryLimitError
	if !errors.As(err, &limit) || result.Content != "draft" || result.Progress.RetryCount != 2 || limit.Progress.ReasoningBytes == 0 {
		t.Fatalf("retry loop escaped its limit or draft was lost: %+v %v", result, err)
	}
}
