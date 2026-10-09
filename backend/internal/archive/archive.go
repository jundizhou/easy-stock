// Package archive 把实时报价与资讯落盘到本地单一专用文件夹，供离线复用与审计。
//
// 目录布局（默认 <dataDir>/data-archive，可用 A_STOCK_ARCHIVE_DIR 覆盖）：
//
//	data-archive/
//	├── realtime/
//	│   ├── 2026-10-09.jsonl   （当日逐条追加，JSON Lines）
//	│   └── latest.json        （最新一次成功快照，覆盖写）
//	└── news/
//	    ├── 2026-10-09.jsonl
//	    └── latest.json
//
// 设计要点：线程安全、原子写（临时文件 + rename）、任何写入失败都只记录不阻断主流程。
package archive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Writer 负责把一类记录追加到专用归档目录。
type Writer[T any] struct {
	mu        sync.Mutex
	dir       string // 该类别子目录，如 .../data-archive/realtime
	latest    string // latest.json 完整路径
	fileMode  os.FileMode
	written   int64
	lastWrite time.Time
	lastError string
}

// New 创建某个类别的归档写入器（如 name="realtime"）。
func New[T any](root string, name string) (*Writer[T], error) {
	if root == "" {
		return nil, fmt.Errorf("archive: root dir is empty")
	}
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("archive: mkdir %q: %w", dir, err)
	}
	return &Writer[T]{
		dir:      dir,
		latest:   filepath.Join(dir, "latest.json"),
		fileMode: 0o644,
	}, nil
}

// Dir 返回该类别归档目录。
func (w *Writer[T]) Dir() string { return w.dir }

// Append 追加一条记录到当日 JSONL，并刷新 latest.json 快照。
// 返回时间戳用于索引；失败时返回错误但不影响调用方主流程（调用方应忽略）。
func (w *Writer[T]) Append(record T) (time.Time, error) {
	now := time.Now()
	line, err := json.Marshal(record)
	if err != nil {
		w.recordError(err)
		return now, fmt.Errorf("archive: marshal: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	daily := filepath.Join(w.dir, now.Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(daily, os.O_CREATE|os.O_WRONLY|os.O_APPEND, w.fileMode)
	if err != nil {
		w.recordErrorLocked(err)
		return now, fmt.Errorf("archive: open %q: %w", daily, err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		w.recordErrorLocked(err)
		return now, fmt.Errorf("archive: write %q: %w", daily, err)
	}
	if err := f.Close(); err != nil {
		w.recordErrorLocked(err)
		return now, fmt.Errorf("archive: close %q: %w", daily, err)
	}

	// 原子刷新 latest.json
	if err := writeAtomic(w.latest, line, w.fileMode); err != nil {
		w.recordErrorLocked(err)
		return now, err
	}

	w.written++
	w.lastWrite = now
	w.lastError = ""
	return now, nil
}

// AppendBatch 追加一批记录（每行一条），并刷新 latest.json 为整批数组。
func (w *Writer[T]) AppendBatch(records []T) (int, error) {
	if len(records) == 0 {
		return 0, nil
	}
	now := time.Now()

	lines := make([][]byte, 0, len(records))
	for _, r := range records {
		line, err := json.Marshal(r)
		if err != nil {
			w.recordError(err)
			return 0, fmt.Errorf("archive: marshal: %w", err)
		}
		lines = append(lines, line)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	daily := filepath.Join(w.dir, now.Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(daily, os.O_CREATE|os.O_WRONLY|os.O_APPEND, w.fileMode)
	if err != nil {
		w.recordErrorLocked(err)
		return 0, fmt.Errorf("archive: open %q: %w", daily, err)
	}
	for _, line := range lines {
		if _, err := f.Write(append(line, '\n')); err != nil {
			f.Close()
			w.recordErrorLocked(err)
			return 0, fmt.Errorf("archive: write %q: %w", daily, err)
		}
	}
	if err := f.Close(); err != nil {
		w.recordErrorLocked(err)
		return 0, fmt.Errorf("archive: close %q: %w", daily, err)
	}

	// latest.json 用整批数组（便于阅读"最近一次长什么样"）
	batch, err := json.MarshalIndent(records, "", "  ")
	if err == nil {
		if err := writeAtomic(w.latest, batch, w.fileMode); err != nil {
			w.recordErrorLocked(err)
		}
	}

	w.written += int64(len(records))
	w.lastWrite = now
	return len(records), nil
}

// Status 返回归档状态快照。
type Status struct {
	Dir       string    `json:"dir"`
	Files     int       `json:"files"`
	Lines     int64     `json:"lines"`
	LastWrite time.Time `json:"last_write,omitempty"`
	LastError string    `json:"last_error,omitempty"`
}

// Report 汇总目录内文件数与总行数。
func (w *Writer[T]) Report() Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	st := Status{Dir: w.dir, LastWrite: w.lastWrite, LastError: w.lastError}
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return st
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		st.Files++
		if data, err := os.ReadFile(filepath.Join(w.dir, e.Name())); err == nil {
			st.Lines += int64(countLines(data))
		}
	}
	return st
}

func (w *Writer[T]) recordError(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.recordErrorLocked(err)
}

func (w *Writer[T]) recordErrorLocked(err error) {
	w.lastError = err.Error()
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return fmt.Errorf("archive: write tmp %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("archive: rename %q: %w", path, err)
	}
	return nil
}

func countLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	n := 0
	for _, b := range data {
		if b == '\n' {
			n++
		}
	}
	if data[len(data)-1] != '\n' {
		n++
	}
	return n
}
