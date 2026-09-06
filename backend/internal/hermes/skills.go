package hermes

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	maxSkillFiles        = 1000
	maxSkillFileBytes    = 8 << 20
	maxSkillTotalBytes   = 64 << 20
	maxSkillArchiveBytes = 128 << 20
)

type SkillImportFile struct {
	Name string
	Data []byte
}

type InstalledSkill struct {
	SkillInfo
	Source string `json:"source"`
	Path   string `json:"path"`
}

func (r *Runtime) ImportSkills(files []SkillImportFile) ([]InstalledSkill, error) {
	if strings.TrimSpace(r.home) == "" {
		return nil, errors.New("Hermes Home 未配置")
	}
	if len(files) == 0 || len(files) > maxSkillFiles {
		return nil, errors.New("没有可导入的 Skill 文件")
	}
	var entries []SkillImportFile
	for _, file := range files {
		isArchive := strings.HasSuffix(strings.ToLower(file.Name), ".zip")
		limit := maxSkillFileBytes
		if isArchive {
			limit = maxSkillArchiveBytes
		}
		if len(file.Data) > limit {
			return nil, fmt.Errorf("文件 %s 超过 %d MB 限制", file.Name, limit/(1<<20))
		}
		if len(file.Data) == 0 {
			continue
		}
		if isArchive {
			expanded, err := expandSkillZip(file)
			if err != nil {
				return nil, err
			}
			entries = append(entries, expanded...)
		} else {
			entries = append(entries, file)
		}
	}
	if len(entries) == 0 || len(entries) > maxSkillFiles {
		return nil, errors.New("导入内容为空或文件数量过多")
	}
	total := 0
	for _, entry := range entries {
		total += len(entry.Data)
		if total > maxSkillTotalBytes {
			return nil, errors.New("导入内容超过 64 MB 限制")
		}
	}
	staging, err := os.MkdirTemp("", "easy-stock-skill-import-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging)
	for _, entry := range entries {
		relative, err := safeRelativePath(entry.Name)
		if err != nil {
			return nil, err
		}
		target := filepath.Join(staging, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, entry.Data, 0o600); err != nil {
			return nil, err
		}
	}
	skills, err := discoverStagedSkills(staging)
	if err != nil {
		return nil, err
	}
	if len(skills) == 0 {
		return nil, errors.New("未找到有效的 SKILL.md")
	}
	for _, item := range skills {
		targetDir := filepath.Join(r.home, "skills", item.Category, item.Name)
		if _, statErr := os.Stat(targetDir); statErr == nil {
			return nil, fmt.Errorf("Skill 已存在: %s", item.Name)
		} else if !os.IsNotExist(statErr) {
			return nil, statErr
		}
	}
	installed := make([]InstalledSkill, 0, len(skills))
	for _, item := range skills {
		targetDir := filepath.Join(r.home, "skills", item.Category, item.Name)
		if err := os.MkdirAll(filepath.Dir(targetDir), 0o700); err != nil {
			return nil, err
		}
		if err := copyDir(item.Dir, targetDir); err != nil {
			return nil, err
		}
		installed = append(installed, InstalledSkill{SkillInfo: item.SkillInfo, Source: "local", Path: targetDir})
	}
	return installed, nil
}

type stagedSkill struct {
	SkillInfo
	Dir string
}

func expandSkillZip(file SkillImportFile) ([]SkillImportFile, error) {
	if len(file.Data) > maxSkillArchiveBytes {
		return nil, errors.New("ZIP 文件超过 128 MB 限制")
	}
	reader, err := zip.NewReader(bytes.NewReader(file.Data), int64(len(file.Data)))
	if err != nil {
		return nil, fmt.Errorf("无法读取 ZIP：%w", err)
	}
	out := make([]SkillImportFile, 0, len(reader.File))
	total := 0
	for _, entry := range reader.File {
		if entry.FileInfo().Mode()&os.ModeSymlink != 0 || strings.HasSuffix(entry.Name, "/") {
			continue
		}
		relative, err := safeRelativePath(entry.Name)
		if err != nil {
			return nil, err
		}
		if len(out) >= maxSkillFiles {
			return nil, errors.New("ZIP 内文件数量过多")
		}
		if entry.UncompressedSize64 > maxSkillFileBytes {
			return nil, fmt.Errorf("ZIP 文件 %s 超过 8 MB 限制", entry.Name)
		}
		rc, err := entry.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(rc, maxSkillFileBytes+1))
		_ = rc.Close()
		if readErr != nil {
			return nil, readErr
		}
		if len(data) > maxSkillFileBytes {
			return nil, fmt.Errorf("ZIP 文件 %s 超过 8 MB 限制", entry.Name)
		}
		total += len(data)
		if total > maxSkillTotalBytes {
			return nil, errors.New("ZIP 解压内容超过 64 MB 限制")
		}
		out = append(out, SkillImportFile{Name: relative, Data: data})
	}
	return out, nil
}

func safeRelativePath(name string) (string, error) {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\\\", "/"))
	if name == "" || !utf8.ValidString(name) || strings.HasPrefix(name, "/") {
		return "", errors.New("Skill 文件路径无效")
	}
	parts := strings.Split(name, "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if part == ".." || strings.ContainsRune(part, 0) || strings.ContainsRune(part, ':') {
			return "", fmt.Errorf("Skill 文件路径包含非法片段: %s", name)
		}
		clean = append(clean, part)
	}
	if len(clean) == 0 {
		return "", errors.New("Skill 文件路径为空")
	}
	return strings.Join(clean, "/"), nil
}

func discoverStagedSkills(root string) ([]stagedSkill, error) {
	var result []stagedSkill
	seen := map[string]bool{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info == nil || info.IsDir() || info.Name() != "SKILL.md" {
			return walkErr
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		name, description := skillFrontmatter(string(data))
		if name == "" {
			return errors.New("SKILL.md 缺少有效的 name")
		}
		if len(name) > 160 || strings.ContainsAny(name, "/\\") {
			return fmt.Errorf("Skill 名称无效: %s", name)
		}
		if seen[name] {
			return fmt.Errorf("Skill 名称重复: %s", name)
		}
		seen[name] = true
		relative, _ := filepath.Rel(root, filepath.Dir(path))
		parts := strings.Split(filepath.ToSlash(relative), "/")
		category := "imported"
		if len(parts) > 1 && parts[0] != "." && parts[0] != "" {
			category = parts[0]
		}
		result = append(result, stagedSkill{SkillInfo: SkillInfo{Name: name, Description: description, Category: category, Enabled: true}, Dir: filepath.Dir(path)})
		return nil
	})
	return result, err
}

func copyDir(source, target string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if info.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Skill 不允许包含符号链接")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0o600)
	})
}
