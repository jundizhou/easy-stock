package runtimelog

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const (
	DefaultMaxBytes = int64(5 * 1024 * 1024)
	DefaultBackups  = 5
	maxMessageRunes = 16 * 1024
)

var (
	bearerPattern = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	queryPattern  = regexp.MustCompile(`(?i)([?&](?:token|api[_-]?key|key|cookie|credential|authorization)=)[^&\s]+`)
	secretPattern = regexp.MustCompile(`(?i)\b(token|api[_-]?key|cookie|credential|authorization)\b["']?\s*[:=]\s*["']?([^"'\s,;&}]+)`)
)

type Writer struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	backups  int
	file     *os.File
	size     int64
}

func NewWriter(directory string, fileName string, maxBytes int64, backups int) (*Writer, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, fmt.Errorf("runtime log directory is required")
	}
	if filepath.Base(fileName) != fileName || strings.TrimSpace(fileName) == "" {
		return nil, fmt.Errorf("runtime log file name must be a base name")
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if backups < 0 {
		backups = DefaultBackups
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create runtime log directory: %w", err)
	}
	w := &Writer{path: filepath.Join(directory, fileName), maxBytes: maxBytes, backups: backups}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Writer) Path() string {
	if w == nil {
		return ""
	}
	return w.path
}

func (w *Writer) Write(p []byte) (int, error) {
	if w == nil {
		return 0, io.ErrClosedPipe
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return 0, io.ErrClosedPipe
	}
	redacted := []byte(Redact(string(p)))
	if w.size > 0 && w.size+int64(len(redacted)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(redacted)
	w.size += int64(n)
	if err != nil {
		return n, err
	}
	// Writers must report the input length when the complete input was accepted,
	// even when redaction changed the number of bytes written to disk.
	return len(p), nil
}

func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *Writer) open() error {
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open runtime log: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("stat runtime log: %w", err)
	}
	w.file = file
	w.size = info.Size()
	return nil
}

func (w *Writer) rotate() error {
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("close runtime log for rotation: %w", err)
	}
	w.file = nil
	if w.backups == 0 {
		if err := os.Remove(w.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove runtime log for rotation: %w", err)
		}
		return w.open()
	}
	for index := w.backups; index >= 1; index-- {
		source := w.path
		if index > 1 {
			source = fmt.Sprintf("%s.%d", w.path, index-1)
		}
		target := fmt.Sprintf("%s.%d", w.path, index)
		_ = os.Remove(target)
		if err := os.Rename(source, target); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("rotate runtime log: %w", err)
		}
	}
	return w.open()
}

func ConfigureStandard(directory string, component string) (*log.Logger, io.Closer, error) {
	writer, err := NewWriter(directory, component+".log", DefaultMaxBytes, DefaultBackups)
	if err != nil {
		return nil, nil, err
	}
	output := io.MultiWriter(os.Stderr, writer)
	flags := log.Ldate | log.Ltime | log.Lmicroseconds | log.LUTC
	logger := log.New(output, "", flags)
	log.SetOutput(output)
	log.SetFlags(flags)
	return logger, writer, nil
}

func Redact(value string) string {
	value = bearerPattern.ReplaceAllString(value, "Bearer <redacted>")
	value = queryPattern.ReplaceAllString(value, "${1}<redacted>")
	value = secretPattern.ReplaceAllString(value, "${1}=<redacted>")
	runes := []rune(value)
	if len(runes) > maxMessageRunes {
		return string(runes[:maxMessageRunes])
	}
	return value
}
