package httpapi

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

var runtimeRequestSequence atomic.Uint64

type requestLogWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *requestLogWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *requestLogWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += int64(n)
	return n, err
}

func (w *requestLogWriter) Flush() {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *requestLogWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	if w.status == 0 {
		w.status = http.StatusSwitchingProtocols
	}
	return hijacker.Hijack()
}

func (w *requestLogWriter) ReadFrom(reader io.Reader) (int64, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if readerFrom, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		n, err := readerFrom.ReadFrom(reader)
		w.bytes += n
		return n, err
	}
	n, err := io.Copy(w.ResponseWriter, reader)
	w.bytes += n
	return n, err
}

func (w *requestLogWriter) Push(target string, options *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, options)
	}
	return http.ErrNotSupported
}

func (w *requestLogWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func runtimeRequestID(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("X-Request-ID"))
	if value != "" && len(value) <= 64 {
		valid := true
		for _, char := range value {
			if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("._-", char) {
				continue
			}
			valid = false
			break
		}
		if valid {
			return value
		}
	}
	return fmt.Sprintf("%x-%x", time.Now().UnixMilli(), runtimeRequestSequence.Add(1))
}

func requestFeature(path string) string {
	switch {
	case path == "/api/health":
		return "health"
	case strings.HasPrefix(path, "/api/v1/themes"), strings.HasPrefix(path, "/api/v1/sector-map"):
		return "theme-radar"
	case strings.HasPrefix(path, "/api/v1/short-term"):
		return "short-term"
	case strings.HasPrefix(path, "/api/v1/stocks/ai-analysis"):
		return "stock-analysis"
	case strings.HasPrefix(path, "/api/v1/portfolio-inspections"):
		return "portfolio-inspection"
	case strings.HasPrefix(path, "/api/v1/stocks"):
		return "stocks"
	case strings.HasPrefix(path, "/api/v1/reviews"):
		return "reviews"
	case strings.HasPrefix(path, "/api/v1/market"), strings.HasPrefix(path, "/api/v1/research"):
		return "market-data"
	case strings.HasPrefix(path, "/api/v1/quotes"):
		return "quotes"
	case strings.HasPrefix(path, "/api/v1/settings"):
		return "settings"
	case strings.HasPrefix(path, "/api/v1/ai"):
		return "ai-chat"
	case strings.HasPrefix(path, "/api/v1/strategy"):
		return "strategy"
	case strings.HasPrefix(path, "/api/v1/ws"):
		return "stream"
	case strings.HasPrefix(path, "/api/v1/sources"):
		return "data-sources"
	default:
		return "http"
	}
}

func (s *Server) logRequest(r *http.Request, writer *requestLogWriter, requestID string, startedAt time.Time) {
	if s == nil || s.logger == nil {
		return
	}
	status := writer.status
	if status == 0 {
		status = http.StatusOK
	}
	level := "info"
	if status >= http.StatusInternalServerError {
		level = "error"
	} else if status >= http.StatusBadRequest {
		level = "warn"
	}
	s.logger.Printf(
		"level=%s event=http_request feature=%q request_id=%q method=%q path=%q status=%d duration_ms=%d response_bytes=%d",
		level,
		requestFeature(r.URL.Path),
		requestID,
		r.Method,
		r.URL.Path,
		status,
		time.Since(startedAt).Milliseconds(),
		writer.bytes,
	)
}
