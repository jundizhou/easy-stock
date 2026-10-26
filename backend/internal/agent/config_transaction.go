package agent

import (
	"errors"
	"os"
	"path/filepath"

	"easy-stock/backend/internal/appsettings"
)

// PrepareSettings holds the runtime selection lock until the settings store
// commits. Failed projections or persistence restore both files and memory.
// On restart, settings.json remains authoritative for the active profile/runtime.
func (s *Service) PrepareSettings(next appsettings.Values, keys map[string]*string, activeKey *string, updateAgent func(AgentSettings) (AgentSettings, error)) (func(bool) error, error) {
	s.mu.Lock()
	finish, err := s.configurationRollback()
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	complete := func(success bool) error {
		defer s.mu.Unlock()
		if success {
			s.active = RuntimeID(next.AgentRuntime)
			return nil
		}
		return finish()
	}
	for id, key := range keys {
		if id != next.ActiveLLMProfileID {
			if err = s.HermesRuntime.StoreLLMProfileKey(id, key); err != nil {
				return nil, errors.Join(err, complete(false))
			}
		}
	}
	if key, ok := keys[next.ActiveLLMProfileID]; ok {
		activeKey = key
	}
	err = s.HermesRuntime.SyncLLMProfile(next.LLM, next.ActiveLLMProfileID, activeKey)
	if err == nil && updateAgent != nil {
		var current AgentSettings
		current, err = s.HermesRuntime.AgentSettings()
		if err == nil {
			current, err = updateAgent(current)
		}
		if err == nil {
			err = s.HermesRuntime.SyncAgentSettings(current)
		}
	}
	if err == nil {
		err = s.codex.SyncConfiguration()
	}
	if err != nil {
		return nil, errors.Join(err, complete(false))
	}
	return complete, nil
}

func (s *Service) configurationRollback() (func() error, error) {
	type savedFile struct {
		path   string
		data   []byte
		exists bool
	}
	var files []savedFile
	paths := []string{filepath.Join(s.home, "config.yaml"), filepath.Join(s.home, ".env")}
	if s.codex.config.Home != "" {
		paths = append(paths, filepath.Join(s.codex.config.Home, "config.toml"))
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		files = append(files, savedFile{path, data, err == nil})
	}
	s.HermesRuntime.mu.RLock()
	cfg, configured, hasKey := s.llm, s.configured, s.hasAPIKey
	s.HermesRuntime.mu.RUnlock()
	return func() error {
		var errs []error
		for _, file := range files {
			if file.exists {
				errs = append(errs, writeSecureFile(file.path, file.data))
			} else if err := os.Remove(file.path); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
		}
		s.HermesRuntime.mu.Lock()
		s.llm, s.configured, s.hasAPIKey = cfg, configured, hasKey
		s.HermesRuntime.mu.Unlock()
		return errors.Join(errs...)
	}, nil
}
