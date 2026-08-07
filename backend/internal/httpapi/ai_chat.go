package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const maxHermesGatewayFrameBytes = 4 << 20

var hermesWebSocketUpgrader = websocket.Upgrader{
	ReadBufferSize:  64 << 10,
	WriteBufferSize: 64 << 10,
	CheckOrigin: func(*http.Request) bool {
		// The server is bound to loopback and protected by a random desktop token.
		// Electron file:// pages do not provide a conventional HTTP origin.
		return true
	},
}

func (s *Server) aiChatWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.hermesGateway == nil {
		writeError(w, http.StatusServiceUnavailable, "Hermes 对话底座不可用")
		return
	}
	status := s.hermesGateway.Status()
	if !status.Available {
		writeError(w, http.StatusServiceUnavailable, firstNonEmpty(status.Message, "Hermes 运行时不可用"))
		return
	}
	if !status.Configured {
		writeError(w, http.StatusPreconditionFailed, firstNonEmpty(status.Message, "请先配置 Hermes 模型"))
		return
	}

	connection, err := hermesWebSocketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer connection.Close()
	connection.SetReadLimit(maxHermesGatewayFrameBytes)

	process, err := s.hermesGateway.Start(r.Context())
	if err != nil {
		_ = writeHermesGatewayError(connection, err.Error())
		return
	}
	waited := false
	defer func() {
		_ = process.Stop()
		if !waited {
			_ = process.Wait()
		}
	}()

	var stderrMu sync.Mutex
	stderrTail := ""
	go func() {
		scanner := bufio.NewScanner(process.Errors())
		scanner.Buffer(make([]byte, 16<<10), 256<<10)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			stderrMu.Lock()
			stderrTail = truncateTail(stderrTail+"\n"+line, 8<<10)
			stderrMu.Unlock()
		}
	}()

	clientDone := make(chan error, 1)
	go func() {
		for {
			messageType, payload, readErr := connection.ReadMessage()
			if readErr != nil {
				clientDone <- readErr
				_ = process.Stop()
				return
			}
			if messageType != websocket.TextMessage {
				continue
			}
			if len(payload) > maxHermesGatewayFrameBytes {
				clientDone <- errors.New("Hermes 请求帧过大")
				_ = process.Stop()
				return
			}
			payload = s.enrichHermesPrompt(r.Context(), payload)
			payload = append(payload, '\n')
			if _, writeErr := process.Input().Write(payload); writeErr != nil {
				clientDone <- writeErr
				_ = process.Stop()
				return
			}
		}
	}()

	scanner := bufio.NewScanner(process.Output())
	scanner.Buffer(make([]byte, 64<<10), maxHermesGatewayFrameBytes)
	for scanner.Scan() {
		payload := append([]byte(nil), scanner.Bytes()...)
		if len(strings.TrimSpace(string(payload))) == 0 {
			continue
		}
		if err := connection.WriteMessage(websocket.TextMessage, payload); err != nil {
			return
		}
	}

	select {
	case <-clientDone:
		return
	default:
	}
	waitErr := process.Wait()
	waited = true
	if scanErr := scanner.Err(); scanErr != nil {
		waitErr = scanErr
	}
	if waitErr != nil && !errors.Is(waitErr, io.EOF) {
		stderrMu.Lock()
		detail := strings.TrimSpace(stderrTail)
		stderrMu.Unlock()
		message := "Hermes 会话意外结束"
		if detail != "" {
			message += ": " + detail
		}
		_ = writeHermesGatewayError(connection, message)
	}
}

func (s *Server) enrichHermesPrompt(parent context.Context, payload []byte) []byte {
	if s.masteryLibrary == nil {
		return payload
	}
	var frame map[string]any
	if err := json.Unmarshal(payload, &frame); err != nil || rpcString(frame["method"]) != "prompt.submit" {
		return payload
	}
	params, ok := frame["params"].(map[string]any)
	if !ok {
		return payload
	}
	prompt := strings.TrimSpace(rpcString(params["text"]))
	if prompt == "" {
		return payload
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	knowledge, err := s.masteryLibrary.ContextForPrompt(ctx, prompt, 12_000)
	if err != nil || strings.TrimSpace(knowledge) == "" {
		return payload
	}
	params["text"] = "[本地游资心法知识库]\n" + knowledge + "\n\n[用户当前问题]\n" + prompt
	updated, err := json.Marshal(frame)
	if err != nil || len(updated) > maxHermesGatewayFrameBytes {
		return payload
	}
	return updated
}

func rpcString(value any) string {
	text, _ := value.(string)
	return text
}

func writeHermesGatewayError(connection *websocket.Conn, message string) error {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "event",
		"params": map[string]any{
			"type": "gateway.error",
			"payload": map[string]string{
				"message": strings.TrimSpace(message),
			},
		},
	})
	if err != nil {
		return err
	}
	return connection.WriteMessage(websocket.TextMessage, payload)
}

func truncateTail(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	return fmt.Sprintf("…%s", value[len(value)-maxBytes:])
}
