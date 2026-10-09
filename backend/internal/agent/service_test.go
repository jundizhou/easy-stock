package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easy-stock/backend/internal/appsettings"
)

func testService(t *testing.T) *Service {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(binary, []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	s := NewService(ServiceConfig{Hermes: HermesConfig{Home: t.TempDir()}, Codex: CodexConfig{Home: t.TempDir(), Executable: binary}})
	key := "old-key"
	if err := s.SyncLLMProfile(appsettings.LLM{Provider: "custom", BaseURL: "https://example.com/v1", Model: "old", APIMode: "responses"}, "profile", &key); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTaskBindingSurvivesRuntimeModelAndSkillChange(t *testing.T) {
	s := testService(t)
	file := filepath.Join(s.home, "skills", "test-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("---\nname: test-skill\ndescription: first skill\n---\nFIRST"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, release, err := s.BindTask(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := s.SelectRuntime(Codex); err != nil {
		t.Fatal(err)
	}
	key := "new-key"
	if err := s.SyncLLMProfile(appsettings.LLM{Provider: "custom", BaseURL: "https://another.example/v1", Model: "new", APIMode: "responses"}, "profile", &key); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(file, []byte("SECOND"), 0600)
	nested, cleanup, err := s.BindTask(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	b := nested.Value(bindingKey{}).(*taskBinding)
	if BoundRuntime(nested) != Hermes || BoundModel(nested) != "old" {
		t.Fatal("task moved to new runtime/model")
	}
	if value, _ := b.hermes.ModelAPIKey(); value != "old-key" {
		t.Fatal("key was not frozen")
	}
	if data, _ := os.ReadFile(filepath.Join(b.hermes.home, "skills", "test-skill", "SKILL.md")); !strings.Contains(string(data), "FIRST") {
		t.Fatal("skill was not frozen")
	}
	next, done, err := s.BindTask(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	if BoundRuntime(next) != Codex || BoundModel(next) != "new" {
		t.Fatal("new task did not switch")
	}
}

func TestTaskBindingFreezesReasoningEffort(t *testing.T) {
	s := testService(t)
	config, err := s.readConfigMap()
	if err != nil {
		t.Fatal(err)
	}
	agentConfig, _ := stringMap(config["agent"])
	agentConfig["easy_stock_reasoning_effort"] = "max"
	if err := s.writeConfigMap(config); err != nil {
		t.Fatal(err)
	}
	ctx, release, err := s.BindTask(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	agentConfig["easy_stock_reasoning_effort"] = "low"
	if err := s.writeConfigMap(config); err != nil {
		t.Fatal(err)
	}
	if effort, ok := BoundReasoningEffort(ctx); !ok || effort != "max" {
		t.Fatal("running task inherited a later reasoning change")
	}
	next, cleanup, err := s.BindTask(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if effort, ok := BoundReasoningEffort(next); !ok || effort != "low" {
		t.Fatal("new task ignored the updated reasoning setting")
	}
}

func TestPreparedConfigurationRollback(t *testing.T) {
	s := testService(t)
	before := s.configurationID()
	key := "new-secret"
	finish, err := s.PrepareSettings(appsettings.Values{AgentRuntime: Codex, ActiveLLMProfileID: "profile", LLM: appsettings.LLM{Provider: "custom", BaseURL: "https://new.example/v1", Model: "new", APIMode: "responses"}}, nil, &key)
	if err != nil {
		t.Fatal(err)
	}
	if err = finish(false); err != nil {
		t.Fatal(err)
	}
	if s.ActiveRuntime() != Hermes || s.configurationID() != before {
		t.Fatal("failed save changed runtime/configuration")
	}
	if actual, _ := s.ModelAPIKey(); actual != "old-key" {
		t.Fatal("failed save changed credential")
	}
	// A failed projection also rolls back before returning to the caller.
	os.Remove(filepath.Join(s.codex.config.Home, "config.toml"))
	os.Mkdir(filepath.Join(s.codex.config.Home, "config.toml"), 0700)
	if err = s.SyncLLM(appsettings.LLM{Model: "bad", APIMode: "responses"}, &key); err == nil {
		t.Fatal("expected projection failure")
	}
	if s.llm.Model != "old" {
		t.Fatal("failed projection changed model")
	}
}

func TestSkillsGenerationChangesOnRemoval(t *testing.T) {
	s := testService(t)
	file := filepath.Join(s.home, "skills", "test-skill", "SKILL.md")
	os.MkdirAll(filepath.Dir(file), 0700)
	os.WriteFile(file, []byte("---\nname: test-skill\ndescription: test\n---\nhello"), 0600)
	first, err := s.codex.skillDigest()
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(filepath.Dir(file), "extra.txt"), []byte("extra"), 0600)
	second, err := s.codex.skillDigest()
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(filepath.Dir(file), "extra.txt"))
	third, err := s.codex.skillDigest()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || first != third {
		t.Fatal("generation ignores skill resources")
	}
}
