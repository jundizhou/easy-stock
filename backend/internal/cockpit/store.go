package cockpit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const maxStoredBriefs = 300

type Brief struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	Phase       Phase  `json:"phase"`
	SessionDate string `json:"session_date"`
	Label       string `json:"label"`
	Trigger     string `json:"trigger"`
	// Caveat 说明数据时点与阶段不一致的情况，例如尚未收盘时生成的盘后复盘。
	Caveat          string `json:"caveat,omitempty"`
	Status          string `json:"status"`
	NarrativeStatus string `json:"narrative_status"`
	Narrative       string `json:"narrative,omitempty"`
	// UnverifiedNumbers 列出叙述中在采集事实里找不到的数值，界面必须提示人工核对。
	UnverifiedNumbers []string  `json:"unverified_numbers,omitempty"`
	Error             string    `json:"error,omitempty"`
	Facts             Facts     `json:"facts"`
	StartedAt         time.Time `json:"started_at"`
	FinishedAt        time.Time `json:"finished_at,omitempty"`
}

// Store 以 JSON 文件保存巡查结果，写入采用临时文件加原子替换，避免中途断电留下半截文件。
type Store struct {
	mu     sync.Mutex
	path   string
	briefs []Brief
}

func OpenStore(path string) (*Store, error) {
	s := &Store{path: path}
	if path == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("读取驾驶舱巡查记录失败: %w", err)
	}
	if err := json.Unmarshal(data, &s.briefs); err != nil {
		return nil, fmt.Errorf("解析驾驶舱巡查记录失败: %w", err)
	}
	for i := range s.briefs {
		if s.briefs[i].Status == "running" {
			s.briefs[i].Status = "failed"
			s.briefs[i].Error = "上次巡查已中断"
			s.briefs[i].FinishedAt = time.Now()
		}
	}
	return s, nil
}

func (s *Store) Put(brief Brief) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.briefs {
		if s.briefs[i].ID == brief.ID {
			s.briefs[i] = brief
			return s.saveLocked()
		}
	}
	s.briefs = append([]Brief{brief}, s.briefs...)
	if len(s.briefs) > maxStoredBriefs {
		s.briefs = s.briefs[:maxStoredBriefs]
	}
	return s.saveLocked()
}

func (s *Store) Get(id string) (Brief, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, brief := range s.briefs {
		if brief.ID == id {
			return brief, true
		}
	}
	return Brief{}, false
}

// List 按时间倒序返回；phase 为空表示全部阶段。
func (s *Store) List(phase Phase, limit int) []Brief {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Brief, 0, limit)
	for _, brief := range s.briefs {
		if phase != "" && brief.Phase != phase {
			continue
		}
		out = append(out, brief)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func (s *Store) HasKey(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, brief := range s.briefs {
		if brief.Key == key {
			return true
		}
	}
	return false
}

// Previous 返回同一会话、同一阶段中最近一次成功的巡查，用于盘中对比变化。
func (s *Store) Previous(phase Phase, sessionDate, excludeID string) (Brief, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, brief := range s.briefs {
		if brief.ID != excludeID && brief.Phase == phase && brief.SessionDate == sessionDate && brief.Status == "ready" {
			return brief, true
		}
	}
	return Brief{}, false
}

func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	data, err := json.Marshal(s.briefs)
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".cockpit-briefs-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), s.path)
}
