package runtimelog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriterRotatesAndBoundsBackups(t *testing.T) {
	directory := t.TempDir()
	w, err := NewWriter(directory, "backend.log", 180, 2)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer w.Close()
	for index := range 12 {
		_, _ = fmt.Fprintf(w, "entry-%d-%s\n", index, strings.Repeat("x", 70))
	}
	for _, name := range []string{"backend.log", "backend.log.1", "backend.log.2"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatalf("missing rotated file %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(directory, "backend.log.3")); !os.IsNotExist(err) {
		t.Fatalf("unexpected extra backup: %v", err)
	}
}

func TestRedactRemovesRuntimeSecrets(t *testing.T) {
	value := Redact(`Authorization: Bearer live-secret token=abc123 api_key:sk-test cookie=session-value /path?key=query-secret&mode=test {"credential":"json-secret"}`)
	for _, secret := range []string{"live-secret", "abc123", "sk-test", "session-value", "query-secret", "json-secret"} {
		if strings.Contains(value, secret) {
			t.Fatalf("secret %q leaked in %q", secret, value)
		}
	}
	if !strings.Contains(value, "mode=test") || !strings.Contains(value, "<redacted>") {
		t.Fatalf("unexpected redacted value: %q", value)
	}
}

func TestWriterRedactsBeforePersisting(t *testing.T) {
	directory := t.TempDir()
	w, err := NewWriter(directory, "backend.log", DefaultMaxBytes, 1)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if _, err := fmt.Fprintln(w, "Authorization: Bearer live-secret api_key=sk-test"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(directory, "backend.log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if strings.Contains(string(content), "live-secret") || strings.Contains(string(content), "sk-test") {
		t.Fatalf("runtime log leaked a secret: %q", content)
	}
}
