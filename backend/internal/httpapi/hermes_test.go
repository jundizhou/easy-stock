package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"sync"

	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/hermes"
)

type fakeHermesGateway struct {
	status       hermes.Status
	promptResult hermes.PromptResult
	promptErr    error
	modelAPIKey  string
	modelKeyErr  error
	start        func(context.Context) (hermes.Process, error)
	lastLLM      appsettings.LLM
	lastKey      *string
}

func (g *fakeHermesGateway) Status() hermes.Status { return g.status }
func (g *fakeHermesGateway) ModelAPIKey() (string, error) {
	return g.modelAPIKey, g.modelKeyErr
}
func (g *fakeHermesGateway) Prompt(context.Context, string) (hermes.PromptResult, error) {
	return g.promptResult, g.promptErr
}
func (g *fakeHermesGateway) Start(ctx context.Context) (hermes.Process, error) {
	if g.start != nil {
		return g.start(ctx)
	}
	return newScriptedHermesProcess(), nil
}
func (g *fakeHermesGateway) SyncLLM(cfg appsettings.LLM, key *string) error {
	g.lastLLM = cfg
	if cfg.Model == "" && cfg.BaseURL == "" && key == nil {
		return nil
	}
	if key == nil {
		g.lastKey = nil
	} else {
		value := *key
		g.lastKey = &value
		g.modelAPIKey = value
		g.status.APIKeyConfigured = value != ""
	}
	g.status.Configured = stringsConfigured(cfg, g.status.APIKeyConfigured)
	return nil
}

func stringsConfigured(cfg appsettings.LLM, hasKey bool) bool {
	return cfg.Model != "" && cfg.BaseURL != "" && (cfg.Provider == "custom" || hasKey)
}

type scriptedHermesProcess struct {
	inputWriter  *io.PipeWriter
	outputReader *io.PipeReader
	errorReader  *io.PipeReader
	done         chan struct{}
	stopOnce     sync.Once
	stop         func()
}

func newScriptedHermesProcess() *scriptedHermesProcess {
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	errorReader, errorWriter := io.Pipe()
	done := make(chan struct{})
	process := &scriptedHermesProcess{
		inputWriter:  inputWriter,
		outputReader: outputReader,
		errorReader:  errorReader,
		done:         done,
	}
	process.stop = func() {
		_ = inputReader.Close()
		_ = inputWriter.Close()
		_ = outputReader.Close()
		_ = outputWriter.Close()
		_ = errorReader.Close()
		_ = errorWriter.Close()
	}
	go func() {
		defer close(done)
		defer outputWriter.Close()
		defer errorWriter.Close()
		writeTestFrame(outputWriter, map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "gateway.ready", "payload": map[string]any{}}})
		scanner := bufio.NewScanner(inputReader)
		for scanner.Scan() {
			var request struct {
				ID     string `json:"id"`
				Method string `json:"method"`
			}
			if json.Unmarshal(scanner.Bytes(), &request) != nil {
				continue
			}
			switch request.Method {
			case "session.create":
				writeTestFrame(outputWriter, map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"session_id": "live-1", "stored_session_id": "stored-1"}})
			case "prompt.submit":
				writeTestFrame(outputWriter, map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "message.delta", "payload": map[string]any{"text": "Hermes "}}})
				writeTestFrame(outputWriter, map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "message.complete", "payload": map[string]any{"content": "Hermes 回复"}}})
				return
			}
		}
	}()
	return process
}

func writeTestFrame(writer io.Writer, frame any) {
	data, _ := json.Marshal(frame)
	_, _ = writer.Write(append(data, '\n'))
}

func (p *scriptedHermesProcess) Input() io.WriteCloser { return p.inputWriter }
func (p *scriptedHermesProcess) Output() io.ReadCloser { return p.outputReader }
func (p *scriptedHermesProcess) Errors() io.ReadCloser { return p.errorReader }
func (p *scriptedHermesProcess) Wait() error {
	<-p.done
	return nil
}
func (p *scriptedHermesProcess) Stop() error {
	p.stopOnce.Do(p.stop)
	return nil
}
