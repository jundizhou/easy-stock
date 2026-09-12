package hermes

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportSkillsDirectoryAndZip(t *testing.T) {
	home := t.TempDir()
	runtime := NewRuntime(Config{Home: home})
	files := []SkillImportFile{
		{Name: "trading/demo/SKILL.md", Data: []byte("---\nname: demo-skill\ndescription: demo\n---\n")},
		{Name: "trading/demo/references/readme.md", Data: []byte("reference")},
	}
	installed, err := runtime.ImportSkills(files)
	if err != nil || len(installed) != 1 {
		t.Fatalf("directory import failed: %v %#v", err, installed)
	}
	if _, err := os.Stat(filepath.Join(home, "skills", "trading", "demo-skill", "references", "readme.md")); err != nil {
		t.Fatalf("imported reference missing: %v", err)
	}

	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	w, _ := zw.Create("research/zip-demo/SKILL.md")
	_, _ = w.Write([]byte("---\nname: zip-demo\ndescription: zip\n---\n"))
	w, _ = zw.Create("research/zip-demo/scripts/run.txt")
	_, _ = w.Write([]byte("not executed"))
	_ = zw.Close()
	installed, err = runtime.ImportSkills([]SkillImportFile{{Name: "zip-demo.zip", Data: archive.Bytes()}})
	if err != nil || len(installed) != 1 {
		t.Fatalf("zip import failed: %v %#v", err, installed)
	}
	if _, err := os.Stat(filepath.Join(home, "skills", "research", "zip-demo", "scripts", "run.txt")); err != nil {
		t.Fatalf("zipped file missing: %v", err)
	}
	settings, err := runtime.AgentSettings()
	if err != nil || len(settings.Skills) != 2 {
		t.Fatalf("imported skills were not discovered by settings: %v %#v", err, settings.Skills)
	}
}

func TestImportSkillsRejectsTraversal(t *testing.T) {
	runtime := NewRuntime(Config{Home: t.TempDir()})
	if _, err := runtime.ImportSkills([]SkillImportFile{{Name: "../SKILL.md", Data: []byte("---\nname: bad\n---\n")}}); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}

func TestDeleteSkillRemovesDirectoryAndDisabledEntry(t *testing.T) {
	home := t.TempDir()
	runtime := NewRuntime(Config{Home: home})
	if _, err := runtime.ImportSkills([]SkillImportFile{
		{Name: "trading/demo/SKILL.md", Data: []byte("---\nname: demo-skill\ndescription: demo\n---\n")},
		{Name: "trading/other/SKILL.md", Data: []byte("---\nname: other-skill\ndescription: other\n---\n")},
	}); err != nil {
		t.Fatalf("import failed: %v", err)
	}
	settings, err := runtime.AgentSettings()
	if err != nil {
		t.Fatalf("agent settings: %v", err)
	}
	for index := range settings.Skills {
		settings.Skills[index].Enabled = settings.Skills[index].Name != "demo-skill"
	}
	if err := runtime.SyncAgentSettings(settings); err != nil {
		t.Fatalf("sync settings: %v", err)
	}

	if err := runtime.DeleteSkill("demo-skill"); err != nil {
		t.Fatalf("delete skill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "skills", "trading", "demo-skill")); !os.IsNotExist(err) {
		t.Fatalf("skill directory still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "skills", "trading", "other-skill", "SKILL.md")); err != nil {
		t.Fatalf("sibling skill was removed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(data), "demo-skill") {
		t.Fatalf("disabled list still references deleted skill: %s", data)
	}
	updated, err := runtime.AgentSettings()
	if err != nil || len(updated.Skills) != 1 || updated.Skills[0].Name != "other-skill" {
		t.Fatalf("unexpected skills after delete: %v %#v", err, updated.Skills)
	}
	if err := runtime.DeleteSkill("missing-skill"); err == nil {
		t.Fatal("expected deleting a missing skill to fail")
	}
	if err := runtime.DeleteSkill("../escape"); err == nil {
		t.Fatal("expected invalid skill name to be rejected")
	}
}
