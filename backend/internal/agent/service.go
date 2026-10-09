package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"easy-stock/backend/internal/appsettings"
)

const Hermes = "hermes"
const Codex = "codex"

var ErrModelProtocolUnsupported = errors.New("MODEL_PROTOCOL_UNSUPPORTED: 当前模型连接不支持 Responses，无法使用 Codex Runtime；请选择支持 Responses 的模型连接或使用 Hermes")

func RuntimeID(value string) string {
	if value == "" {
		return Hermes
	}
	return value
}

func SupportsResponses(cfg appsettings.LLM) bool {
	return cfg.APIMode == "responses" || cfg.APIMode == "codex_responses"
}

// Service owns the one application configuration and dispatches both runtimes.
// The legacy home remains in place so upgrades preserve native Hermes sessions.
// Its config, secrets and skills are application-owned shared inputs, never a
// second set of user-editable runtime settings.
type Service struct {
	mu sync.RWMutex
	*HermesRuntime
	codex      *CodexRuntime
	active     string
	startupErr error
}

type ServiceConfig struct {
	Hermes HermesConfig
	Codex  CodexConfig
}

func NewService(cfg ServiceConfig) *Service {
	h := NewHermesRuntime(cfg.Hermes)
	s := &Service{HermesRuntime: h, active: Hermes}
	s.codex = NewCodexRuntime(cfg.Codex, h)
	return s
}

func (s *Service) ActiveRuntime() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

func (s *Service) SelectRuntime(id string) error {
	id = RuntimeID(id)
	if id != Hermes && id != Codex {
		return fmt.Errorf("不支持的运行引擎: %s", id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == Codex {
		status := s.codex.Status()
		if !status.Available || !status.Configured {
			return errors.New(status.Message)
		}
	}
	s.active = id
	return nil
}

// RestoreSelection never silently falls back if a saved runtime cannot run.
func (s *Service) RestoreSelection(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active = RuntimeID(id)
}

func (s *Service) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	status := s.activeStatus()
	status.ConfigurationID = s.configurationID()
	return status
}

func (s *Service) configurationID() string {
	hash := sha256.New()
	hash.Write([]byte(s.active))
	for _, name := range []string{"config.yaml", ".env"} {
		if data, err := os.ReadFile(filepath.Join(s.home, name)); err == nil {
			hash.Write(data)
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (s *Service) activeStatus() Status {
	if s.startupErr != nil {
		return Status{Runtime: s.active, Message: s.startupErr.Error()}
	}
	if s.active == Codex {
		return s.codex.Status()
	}
	if s.active != Hermes {
		return Status{Runtime: s.active, Message: "不支持的运行引擎"}
	}
	return s.HermesRuntime.Status()
}

func (s *Service) RuntimeStatuses() map[string]Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]Status{Hermes: s.HermesRuntime.Status(), Codex: s.codex.Status()}
}

func (s *Service) ValidateSelection(id string, cfg appsettings.LLM) error {
	if RuntimeID(id) != Hermes && RuntimeID(id) != Codex {
		return errors.New("不支持的运行引擎")
	}
	if RuntimeID(id) == Codex {
		if !SupportsResponses(cfg) {
			return ErrModelProtocolUnsupported
		}
		if !s.codex.Status().Available {
			return errors.New("Codex 运行时不可用，请检查安装包")
		}
	}
	return nil
}

func (s *Service) SyncLLM(cfg appsettings.LLM, key *string) error {
	return s.SyncLLMProfile(cfg, "active", key)
}

func (s *Service) SyncLLMProfile(cfg appsettings.LLM, profile string, key *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rollback, err := s.configurationRollback()
	if err != nil {
		return err
	}
	if err = s.HermesRuntime.SyncLLMProfile(cfg, profile, key); err == nil {
		err = s.codex.SyncConfiguration()
	}
	if err != nil {
		return errors.Join(err, rollback())
	}
	return nil
}

func (s *Service) SyncAgentSettings(settings AgentSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rollback, err := s.configurationRollback()
	if err != nil {
		return err
	}
	if err = s.HermesRuntime.SyncAgentSettings(settings); err == nil {
		err = s.codex.SyncConfiguration()
	}
	if err != nil {
		return errors.Join(err, rollback())
	}
	return nil
}

func (s *Service) ImportSkills(files []SkillImportFile) ([]InstalledSkill, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.HermesRuntime.ImportSkills(files)
	if err == nil {
		err = s.codex.SyncConfiguration()
	}
	return items, err
}

func (s *Service) DeleteSkill(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.HermesRuntime.DeleteSkill(name); err != nil {
		return err
	}
	return s.codex.SyncConfiguration()
}

func (s *Service) Start(ctx context.Context) (Process, error) { return s.StartWithRevision(ctx, "") }

func (s *Service) StartWithRevision(ctx context.Context, expected string) (Process, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if expected != "" && expected != s.configurationID() {
		return nil, errors.New("运行引擎或模型设置已变化，请重新发送；本次未调用模型")
	}
	if s.active == Codex {
		return s.codex.Start(ctx)
	}
	if s.active != Hermes {
		return nil, errors.New("不支持的运行引擎")
	}
	return s.HermesRuntime.Start(ctx)
}

func (s *Service) Prompt(ctx context.Context, prompt string) (PromptResult, error) {
	return s.PromptWithOptions(ctx, prompt, PromptOptions{})
}

func (s *Service) PromptWithBrowserState(ctx context.Context, prompt, state string) (PromptResult, error) {
	return s.PromptWithOptions(ctx, prompt, PromptOptions{Sandbox: true, AutoApprove: true, BrowserStatePath: state, Toolsets: []string{"web"}})
}

func (s *Service) PromptWithOptionsAndBrowserState(ctx context.Context, prompt, state string, options PromptOptions) (PromptResult, error) {
	options.BrowserStatePath = state
	return s.PromptWithOptions(ctx, prompt, options)
}

type taskBinding struct {
	owner    *Service
	runtime  string
	hermes   *HermesRuntime
	codex    *CodexRuntime
	identity string
	status   Status
	effort   string
}
type bindingKey struct{}

// BindTask pins an entire business workflow, including its retries and nested
// stock analyses. Tests and lightweight integrations can omit the capability.
func BindTask(ctx context.Context, p Prompter) (context.Context, func(), error) {
	if binder, ok := p.(interface {
		BindTask(context.Context) (context.Context, func(), error)
	}); ok {
		return binder.BindTask(ctx)
	}
	return ctx, func() {}, nil
}

func (s *Service) BindTask(ctx context.Context) (context.Context, func(), error) {
	if b, ok := ctx.Value(bindingKey{}).(*taskBinding); ok && b.owner == s {
		return ctx, func() {}, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	dir, err := os.MkdirTemp("", "easy-stock-agent-snapshot-")
	if err != nil {
		return ctx, func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	for _, name := range []string{"config.yaml", ".env", "model-capabilities.json"} {
		data, readErr := os.ReadFile(filepath.Join(s.home, name))
		if os.IsNotExist(readErr) {
			continue
		}
		if readErr != nil {
			cleanup()
			return ctx, func() {}, readErr
		}
		if err = writeSecureFile(filepath.Join(dir, name), data); err != nil {
			cleanup()
			return ctx, func() {}, err
		}
	}
	if _, statErr := os.Stat(filepath.Join(s.home, "skills")); statErr == nil {
		if err := copySkillDirectory(filepath.Join(s.home, "skills"), filepath.Join(dir, "skills")); err != nil {
			cleanup()
			return ctx, func() {}, err
		}
	}
	s.HermesRuntime.mu.RLock()
	cfg, configured, key := s.llm, s.configured, s.hasAPIKey
	s.HermesRuntime.mu.RUnlock()
	h := NewHermesRuntime(HermesConfig{RuntimeRoot: s.runtimeRoot, Home: dir, WorkDir: s.workDir, PythonPath: s.pythonPath})
	h.llm, h.configured, h.hasAPIKey = cfg, configured, key
	config, err := h.readConfigMap()
	if err != nil {
		cleanup()
		return ctx, func() {}, err
	}
	c := NewCodexRuntime(s.codex.config, h)
	b := &taskBinding{owner: s, runtime: s.active, hermes: h, codex: c, identity: ModelIdentity(s.active, cfg), status: s.activeStatus(), effort: storedReasoningEffort(config)}
	return context.WithValue(ctx, bindingKey{}, b), cleanup, nil
}

func (s *Service) PromptWithOptions(ctx context.Context, prompt string, options PromptOptions) (PromptResult, error) {
	bound, cleanup, err := s.BindTask(ctx)
	if err != nil {
		return PromptResult{}, err
	}
	defer cleanup()
	b := bound.Value(bindingKey{}).(*taskBinding)
	var result PromptResult
	if b.runtime == Codex {
		result, err = b.codex.PromptWithOptions(bound, prompt, options)
	} else {
		result, err = b.hermes.PromptWithOptions(bound, prompt, options)
	}
	result.Runtime, result.ModelIdentity = b.runtime, b.identity
	return result, err
}

func ModelIdentity(runtime string, cfg appsettings.LLM) string {
	cfg.APIKey = ""
	data, _ := json.Marshal([]any{RuntimeID(runtime), cfg})
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func BoundRuntime(ctx context.Context) string {
	if b, ok := ctx.Value(bindingKey{}).(*taskBinding); ok {
		return b.runtime
	}
	return ""
}

func BoundModel(ctx context.Context) string {
	if binding, ok := ctx.Value(bindingKey{}).(*taskBinding); ok {
		return binding.hermes.llm.Model
	}
	return ""
}

// The bound configuration is copied at task start. Its digest includes the
// reasoning policy, so a resumed research cannot silently mix configurations.
func BoundConfigurationIdentity(ctx context.Context) string {
	if binding, ok := ctx.Value(bindingKey{}).(*taskBinding); ok {
		data, err := os.ReadFile(filepath.Join(binding.hermes.home, "config.yaml"))
		if err != nil {
			return binding.identity
		}
		hash := sha256.Sum256(data)
		return binding.identity + ":" + hex.EncodeToString(hash[:])
	}
	return ""
}

func BoundStatus(ctx context.Context) (Status, bool) {
	if binding, ok := ctx.Value(bindingKey{}).(*taskBinding); ok {
		return binding.status, true
	}
	return Status{}, false
}

func BoundLLM(ctx context.Context) (appsettings.LLM, bool) {
	if binding, ok := ctx.Value(bindingKey{}).(*taskBinding); ok {
		return binding.hermes.llm, true
	}
	return appsettings.LLM{}, false
}

func BoundReasoningEffort(ctx context.Context) (string, bool) {
	if binding, ok := ctx.Value(bindingKey{}).(*taskBinding); ok {
		return binding.effort, true
	}
	return "", false
}
