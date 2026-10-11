package execution

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AuditRecord 是一条 append-only 审计记录。执行网关的每一次关键动作
// （目标生成、意图受理、成交登记、急停拉闸）都会落一条，用于事后追溯与合规复核。
//
// 设计要点：只追加、不修改、不删除；文件为空或缺失时读取返回空切片而非错误。
type AuditRecord struct {
	TS       string          `json:"ts"`                  // RFC3339 时间戳
	Type     string          `json:"type"`                // target_generated | intent_accepted | fill_recorded | killswitch_engaged | killswitch_disengaged
	Mode     string          `json:"mode"`                // sim | live
	Strategy string          `json:"strategy,omitempty"`  // core2
	SignalID string          `json:"signal_id,omitempty"` // 信号指纹
	IntentID string          `json:"intent_id,omitempty"` // 意图编号
	FillID   string          `json:"fill_id,omitempty"`   // 成交编号
	Detail   json.RawMessage `json:"detail,omitempty"`    // 动作相关的结构化载荷
}

// Auditor 是 JSONL 追加审计器，线程安全（单文件串行写入）。
type Auditor struct {
	mu   sync.Mutex
	path string
}

// NewAuditor 构造审计器。path 为空时退化为无操作（所有方法安全返回）。
func NewAuditor(path string) *Auditor {
	return &Auditor{path: strings.TrimSpace(path)}
}

// Append 追加一条审计记录（自动补时间戳、自动建目录）。
func (a *Auditor) Append(rec AuditRecord) error {
	if a.path == "" {
		return nil
	}
	if rec.TS == "" {
		rec.TS = time.Now().Format(time.RFC3339)
	}
	if dir := filepath.Dir(a.path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = f.Write(b)
	return err
}

// Read 读取最近 limit 条审计记录（limit<=0 表示全部）。文件缺失时返回空切片。
func (a *Auditor) Read(limit int) ([]AuditRecord, error) {
	if a.path == "" {
		return []AuditRecord{}, nil
	}
	raw, err := os.ReadFile(a.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []AuditRecord{}, nil
		}
		return nil, err
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return []AuditRecord{}, nil
	}
	lines := strings.Split(text, "\n")
	records := make([]AuditRecord, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec AuditRecord
		if json.Unmarshal([]byte(line), &rec) == nil {
			records = append(records, rec)
		}
	}
	if limit > 0 && len(records) > limit {
		records = records[len(records)-limit:]
	}
	return records, nil
}

// Count 返回审计记录条数（用于状态展示）。
func (a *Auditor) Count() (int, error) {
	records, err := a.Read(0)
	if err != nil {
		return 0, err
	}
	return len(records), nil
}
