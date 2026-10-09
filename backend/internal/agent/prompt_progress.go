package agent

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Metrics deliberately omit model text and credentials.
type PromptProgress struct {
	Event           string `json:"event,omitempty"`
	ElapsedMS       int64  `json:"elapsed_ms"`
	FirstResponseMS int64  `json:"first_response_ms,omitempty"`
	TextBytes       int    `json:"text_bytes"`
	ReasoningBytes  int    `json:"reasoning_bytes"`
	RetryCount      int    `json:"retry_count"`
}

type PromptTimeoutError struct {
	Kind          string
	Wait          time.Duration
	Progress      PromptProgress
	RuntimeDetail string
}

func (e *PromptTimeoutError) Error() string {
	label := "等待模型首个有效响应"
	if e.Kind == "idle" {
		label = "模型持续无有效输出"
	}
	message := fmt.Sprintf("%s超过%d秒（已接收正文%d字节、思考%d字节、已观测重试%d次）", label, int(e.Wait.Seconds()), e.Progress.TextBytes, e.Progress.ReasoningBytes, e.Progress.RetryCount)
	if e.RuntimeDetail != "" {
		message += "；运行时详情：" + e.RuntimeDetail
	}
	return message
}
func (e *PromptTimeoutError) Unwrap() error { return context.DeadlineExceeded }

type PromptRetryLimitError struct {
	MaxAttempts int
	Progress    PromptProgress
}

func (e *PromptRetryLimitError) Error() string {
	return fmt.Sprintf("模型当前阶段已达%d次尝试上限，停止重复生成（已接收正文%d字节、思考%d字节）", e.MaxAttempts, e.Progress.TextBytes, e.Progress.ReasoningBytes)
}

type promptWatchdog struct {
	options      PromptOptions
	started      time.Time
	timer        *time.Timer
	kind         string
	wait         time.Duration
	received     bool
	lastNotified time.Time
	progress     PromptProgress
	lastRetry    string
}

func newPromptWatchdog(options PromptOptions) *promptWatchdog {
	w := &promptWatchdog{options: options, started: time.Now()}
	w.reset("first_response", options.FirstResponseTimeout)
	return w
}
func (w *promptWatchdog) reset(kind string, wait time.Duration) {
	w.kind, w.wait = kind, wait
	if wait <= 0 {
		if w.timer != nil {
			w.timer.Stop()
			w.timer = nil
		}
		return
	}
	if w.timer == nil {
		w.timer = time.NewTimer(wait)
		return
	}
	if !w.timer.Stop() {
		select {
		case <-w.timer.C:
		default:
		}
	}
	w.timer.Reset(wait)
}
func (w *promptWatchdog) channel() <-chan time.Time {
	if w.timer == nil {
		return nil
	}
	return w.timer.C
}
func (w *promptWatchdog) close() {
	if w.timer != nil {
		w.timer.Stop()
	}
}
func (w *promptWatchdog) snapshot() PromptProgress {
	p := w.progress
	p.ElapsedMS = time.Since(w.started).Milliseconds()
	return p
}
func (w *promptWatchdog) timeout() error {
	return &PromptTimeoutError{Kind: w.kind, Wait: w.wait, Progress: w.snapshot()}
}
func (w *promptWatchdog) retryLimitError() error {
	if w.options.MaxAttempts > 0 && w.progress.RetryCount >= w.options.MaxAttempts {
		return &PromptRetryLimitError{MaxAttempts: w.options.MaxAttempts, Progress: w.snapshot()}
	}
	return nil
}
func (w *promptWatchdog) observe(frame rpcFrame) {
	event := eventType(frame)
	text := firstNonEmpty(eventText(frame, "delta"), eventText(frame, "text"), eventText(frame, "content"))
	active := false
	retrying := false
	switch event {
	case "message.delta":
		w.progress.TextBytes += len(text)
		active = text != ""
	case "reasoning.delta":
		w.progress.ReasoningBytes += len(text)
		active = text != ""
	case "message.complete":
		active = text != ""
		if len(text) > w.progress.TextBytes {
			w.progress.TextBytes = len(text)
		}
	case "notification.show", "status.update", "thinking.delta":
		// Retry/wait notices are diagnostics, never evidence of generation.
		status := firstNonEmpty(eventText(frame, "kind"), eventText(frame, "key")) + " " + text
		lower := strings.ToLower(status)
		if (strings.Contains(lower, "retry") || strings.Contains(lower, "重试")) && status != w.lastRetry {
			w.lastRetry = status
			w.progress.RetryCount++
			w.progress.Event = "retry"
			retrying = true
		}
	}
	if active {
		if !w.received {
			w.received = true
			w.progress.FirstResponseMS = time.Since(w.started).Milliseconds()
		}
		w.progress.Event = event
		w.reset("idle", w.options.IdleTimeout)
	}
	if w.options.OnProgress != nil && (active || retrying) && (w.lastNotified.IsZero() || time.Since(w.lastNotified) >= 5*time.Second || retrying || event == "message.complete") {
		w.lastNotified = time.Now()
		w.options.OnProgress(w.snapshot())
	}
}
