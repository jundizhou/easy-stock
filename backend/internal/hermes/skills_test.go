package hermes

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
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
