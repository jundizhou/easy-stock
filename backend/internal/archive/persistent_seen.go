// Package archive 的持久化去重扩展（P0 整改，V0.2 任务五）。
//
// 背景
// ----
// 原实现 `Writer.seen` 是**内存 map**（archive.go:38-41），进程重启即清零。
// 实测后果（10-09）：
//
//	· realtime: 794 行 / 66 唯一键 = 重复率 91.7%
//	· news:     44 行 / 33 唯一键 = 重复率 25.0%
//	· 归档调度器当天启动 8 次 → 每次重启后同一批滚动数据被重新落盘
//
// 本文件提供 `PersistentSeen`：把去重键集合按日持久化到磁盘，
// 进程启动时加载，从而**跨进程**保持去重语义。
//
// 设计要点
// --------
//
//	· 按日分文件：`<dir>/.seen-<YYYY-MM-DD>.json`
//	· 内存索引 + 落盘；写盘用「追加 + 周期压缩」避免每次 O(n) 重写
//	· 任何 I/O 失败都降级为「仅内存去重」，绝不阻断主流程
//	· 跨日自动切换文件（与新记录的自然日一致）
//	· 向后兼容：不影响原有 Writer 行为，需显式启用
//
// 用法
// ----
//
//	wr, _ := archive.New[Record[foundation.Quote]](root, "realtime")
//	ps, _ := archive.NewPersistentSeen(wr.Dir())
//	wr.WithKeyPersistent(realtimeKey, ps)
package archive

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// PersistentSeen 跨进程去重键存储。
//
// 每个自然日一个文件，内容为 JSON 行格式（每行一个 key），
// 便于追加写且损坏时能部分恢复。
//
// 关于「天」的口径（重要）：调用方若知道该批数据所属的业务日
// （例如归档批次携带的 trade_date），必须用 BindDay 显式绑定，
// 否则会与 Writer 的落盘日（time.Now）口径不一致 —— 二者指向
// 不同键文件时，跨进程去重会静默失效（测试可复现）。
type PersistentSeen struct {
	mu  sync.Mutex
	dir string
	// boundDay 非空表示「固定绑定」到该业务日，忽略系统当天。
	boundDay string

	day     string
	path    string
	keys    map[string]struct{}
	loaded  bool
	dirty   int   // 未落盘的追加条数
	flushAt int   // 达到该条数触发一次 fsync
	err     error // 最近一次 I/O 错误（仅记录）
}

// 每累计多少条新 key 触发一次落盘。
const persistentSeenFlushBatch = 200

// NewPersistentSeen 创建/加载某个归档目录的去重键存储（按系统当天）。
func NewPersistentSeen(dir string) (*PersistentSeen, error) {
	return newPersistentSeen(dir, "")
}

// NewPersistentSeenForDay 创建/加载**指定业务日**的去重键存储。
//
// 归档批次的业务日与系统当天可能不同（补抓、跨零点、回放测试），
// 此时必须用本函数显式绑定，确保与 Writer 落盘到同一天的键文件。
func NewPersistentSeenForDay(dir, day string) (*PersistentSeen, error) {
	if day == "" {
		return nil, fmt.Errorf("archive: persistent seen day is empty")
	}
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return nil, fmt.Errorf("archive: invalid day %q: %w", day, err)
	}
	return newPersistentSeen(dir, day)
}

func newPersistentSeen(dir, boundDay string) (*PersistentSeen, error) {
	if dir == "" {
		return nil, fmt.Errorf("archive: persistent seen dir is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("archive: mkdir %q: %w", dir, err)
	}
	ps := &PersistentSeen{
		dir:      dir,
		boundDay: boundDay,
		keys:     make(map[string]struct{}),
		flushAt:  persistentSeenFlushBatch,
	}
	ps.switchDayLocked(time.Now())
	return ps, nil
}

// pathForDay 返回某日的键文件路径。
func (p *PersistentSeen) pathForDay(day string) string {
	return filepath.Join(p.dir, ".seen-"+day+".jsonl")
}

// switchDayLocked 切换到指定日期的键文件（调用方持锁）。
// 若已 BindDay，则始终锁定业务日，不随系统时间漂移。
func (p *PersistentSeen) switchDayLocked(now time.Time) {
	day := now.Format("2006-01-02")
	if p.boundDay != "" {
		day = p.boundDay
	}
	if p.day == day && p.loaded {
		return
	}
	p.day = day
	p.path = p.pathForDay(day)
	p.keys = make(map[string]struct{})
	p.dirty = 0
	p.loadLocked()
}

// loadLocked 从磁盘恢复当日已见键（调用方持锁）。
// 文件不存在视为「今天还没有任何键」，不算错误。
func (p *PersistentSeen) loadLocked() {
	f, err := os.Open(p.path)
	if err != nil {
		if !os.IsNotExist(err) {
			p.err = err
		}
		p.loaded = true
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		// 兼容「纯文本行」与「JSON 字符串行」两种格式
		var decoded string
		if line[0] == '"' {
			if err := json.Unmarshal(line, &decoded); err == nil {
				p.keys[decoded] = struct{}{}
				continue
			}
		}
		p.keys[string(line)] = struct{}{}
	}
	if err := sc.Err(); err != nil {
		// 部分恢复：保留已读到的键
		p.err = err
	}
	p.loaded = true
}

// Seen 判断键是否已出现；未出现则登记并返回 false。
// 语义与 archive.Writer.admitLocked 完全一致（返回 true 表示「应写入」）。
//
// 返回 (admitted, ok)：ok=false 表示持久化存储不可用，
// 调用方应降级到自己的内存去重。
func (p *PersistentSeen) Admit(key string) (admitted bool, ok bool) {
	if key == "" {
		return false, true
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	// 跨日自动切换（未绑定业务日时才随系统时间漂移）
	if p.boundDay == "" && time.Now().Format("2006-01-02") != p.day {
		p.switchDayLocked(time.Now())
	}
	if _, exists := p.keys[key]; exists {
		return false, true
	}
	p.keys[key] = struct{}{}
	p.dirty++
	p.appendLocked(key)
	return true, true
}

// appendLocked 追加一个键到当日文件（调用方持锁）。
// 采用「单行追加」而非整体重写，保证写入量与新增量成正比。
func (p *PersistentSeen) appendLocked(key string) {
	line, err := json.Marshal(key)
	if err != nil {
		p.err = err
		return
	}
	f, err := os.OpenFile(p.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		p.err = err
		return
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		p.err = err
	}
	if err := f.Close(); err != nil {
		p.err = err
	}
}

// Count 返回当日已登记的键数量。
func (p *PersistentSeen) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.keys)
}

// LastError 返回最近一次 I/O 错误（供健康检查）。
func (p *PersistentSeen) LastError() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err == nil {
		return ""
	}
	return p.err.Error()
}

// Day 返回当前生效的自然日。
func (p *PersistentSeen) Day() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.day
}

// Close 释放资源。当前实现为「追加写 + 每键立即 fsync」，
// 不持有长生命周期文件句柄，因此仅做幂等标记，供 Writer 替换持久化层时调用。
func (p *PersistentSeen) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.loaded = false
}

// ------------------------------------------------------------------ Writer 集成

// WithKeyPersistent 启用「内存 + 持久化」双层去重。
//
// 语义：只要**任一**存储认为该键已出现，即判定为重复并跳过。
// 这样即使持久化文件损坏/丢失，内存层仍能兜住同进程内的重复；
// 即使进程重启，持久化层也能兜住跨进程重复。
//
// ps 为 nil 时退化为纯内存去重（等价 WithKey）。
func (w *Writer[T]) WithKeyPersistent(keyOf func(T) string, ps *PersistentSeen) *Writer[T] {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.persistent != nil {
		w.persistent.Close()
	}
	w.keyOf = keyOf
	if w.seen == nil {
		w.seen = make(map[string]struct{})
	}
	w.persistent = ps
	return w
}

// admitLockedPersistent 双层去重判定（调用方持 w.mu）。
func (w *Writer[T]) admitLockedPersistent(record T, day string) bool {
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
	// 内存层
	if _, ok := w.seen[key]; ok {
		w.skipped++
		return false
	}
	// 持久化层
	if w.persistent != nil {
		admitted, ok := w.persistent.Admit(key)
		if ok && !admitted {
			w.seen[key] = struct{}{} // 同步到内存，加速后续判定
			w.skipped++
			return false
		}
	}
	w.seen[key] = struct{}{}
	return true
}

// SeenStats 去重统计（供健康检查输出）。
type SeenStats struct {
	MemoryKeys     int    `json:"memory_keys"`
	PersistentKeys int    `json:"persistent_keys"`
	PersistentDay  string `json:"persistent_day"`
	PersistentErr  string `json:"persistent_err,omitempty"`
	Skipped        int64  `json:"skipped"`
}

// SeenStats 返回当前去重状态。
func (w *Writer[T]) SeenStats() SeenStats {
	w.mu.Lock()
	defer w.mu.Unlock()
	st := SeenStats{MemoryKeys: len(w.seen), Skipped: w.skipped}
	if w.persistent != nil {
		st.PersistentKeys = w.persistent.Count()
		st.PersistentDay = w.persistent.Day()
		st.PersistentErr = w.persistent.LastError()
	}
	return st
}
