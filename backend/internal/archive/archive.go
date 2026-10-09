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

	// [ES_ARCHIVE_DEDUP] 跨周期去重：数据源只提供滚动窗口（如财联社 ~20 条），
	// 逐周期拉取会反复落盘同一批记录。这里按 key 记住「今天已写过的」，
	// 只追加新增，避免存档被重复行稀释（实测 44 行里仅 33 条唯一）。
	keyOf   func(T) string     // nil 表示不去重
	seen    map[string]struct{} // 当日已写 key
	seenDay string              // seen 对应的日期，跨日自动重置
	skipped int64               // 累计被去重跳过的条数
}

// WithKey 启用按 key 跨周期去重。key 为空字符串的记录会被跳过。
func (w *Writer[T]) WithKey(keyOf func(T) string) *Writer[T] {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.keyOf = keyOf
	if w.seen == nil {
		w.seen = make(map[string]struct{})
	}
	return w
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

// admitLocked 判断一条记录是否应写入（去重）。返回 false 表示跳过。
// 调用方必须已持有 w.mu。跨日自动重置 seen。
func (w *Writer[T]) admitLocked(record T, day string) bool {
	if w.keyOf == nil {
		return true
	}
	if w.seenDay != day {
		w.seen = make(map[string]struct{})
		w.seenDay = day
	}
	key := w.keyOf(record)
	if key == "" {
		return false
	}
	if _, ok := w.seen[key]; ok {
		w.skipped++
		return false
	}
	w.seen[key] = struct{}{}
	return true
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

	day := now.Format("2006-01-02")
	// [ES_ARCHIVE_DEDUP] 跨周期去重：同一批滚动资讯不重复落盘。
	if !w.admitLocked(record, day) {
		return now, nil
	}

	daily := filepath.Join(w.dir, day+".jsonl")
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
// 启用去重时，整批中「今日已写过」或「批内重复」的记录会被跳过。
func (w *Writer[T]) AppendBatch(records []T) (int, error) {
	if len(records) == 0 {
		return 0, nil
	}
	now := time.Now()
	day := now.Format("2006-01-02")

	w.mu.Lock()
	defer w.mu.Unlock()

	// [ES_ARCHIVE_DEDUP] 先过滤，再序列化，避免为重复记录做无谓 marshal。
	kept := records
	if w.keyOf != nil {
		kept = make([]T, 0, len(records))
		for _, r := range records {
			if w.admitLocked(r, day) {
				kept = append(kept, r)
			}
		}
		if len(kept) == 0 {
			w.lastWrite = now
			return 0, nil
		}
	}

	lines := make([][]byte, 0, len(kept))
	for _, r := range kept {
		line, err := json.Marshal(r)
		if err != nil {
			w.recordError(err)
			return 0, fmt.Errorf("archive: marshal: %w", err)
		}
		lines = append(lines, line)
	}

	daily := filepath.Join(w.dir, day+".jsonl")
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
	batch, err := json.MarshalIndent(kept, "", "  ")
	if err == nil {
		if err := writeAtomic(w.latest, batch, w.fileMode); err != nil {
			w.recordErrorLocked(err)
		}
	}

	w.written += int64(len(kept))
	w.lastWrite = now
	return len(kept), nil
}

// Status 返回归档状态快照。
type Status struct {
	Dir       string    `json:"dir"`
	Files     int       `json:"files"`
	Lines     int64     `json:"lines"`
	LastWrite time.Time `json:"last_write,omitempty"`
	LastError string    `json:"last_error,omitempty"`
	// [ES_ARCHIVE_DEDUP] 本次进程内被去重跳过的累计条数，便于观察「重复率」。
	Skipped int64 `json:"skipped,omitempty"`
}

// Report 汇总目录内文件数与总行数。
func (w *Writer[T]) Report() Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	st := Status{Dir: w.dir, LastWrite: w.lastWrite, LastError: w.lastError, Skipped: w.skipped}
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
