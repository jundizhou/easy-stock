package stockanalysis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/runtimelog"
)

// Parsed intermediate JSON is private, unvalidated working state. It never
// becomes an AI report until the complete synthesis passes validation.
type ResearchCheckpoint struct {
	PromptVersion      string                          `json:"prompt_version"`
	CompressionVersion string                          `json:"compression_version"`
	ModelIdentity      string                          `json:"model_identity"`
	Request            ResearchRequest                 `json:"request"`
	SnapshotHash       string                          `json:"snapshot_hash"`
	Outline            *ResearchOutline                `json:"outline,omitempty"`
	Outputs            map[string]ResearchStageOutput  `json:"outputs,omitempty"`
	Failures           map[string]ResearchStageFailure `json:"failures,omitempty"`
	Attempts           []ResearchAttempt               `json:"attempts,omitempty"`
	RepairUsed         bool                            `json:"repair_used,omitempty"`
}
type ResearchStageOutput struct {
	PromptHash string          `json:"prompt_hash"`
	Value      json.RawMessage `json:"value"`
}

// Failed final output is private diagnostic data, never a successful stage or
// an AI report. Reasoning and model credentials are not captured here.
type ResearchStageFailure struct {
	PromptHash string `json:"prompt_hash"`
	Error      string `json:"error"`
	Content    string `json:"content"`
}
type researchExecutionKey struct{}
type researchExecution struct {
	resume     *ResearchJob
	checkpoint *ResearchCheckpoint
	identity   string
	save       func(*ResearchCheckpoint) error
}

func ResearchResumeData(ctx context.Context) *ResearchJob {
	if execution, ok := ctx.Value(researchExecutionKey{}).(*researchExecution); ok {
		return execution.resume
	}
	return nil
}
func WithResearchModelIdentity(ctx context.Context, identity string) context.Context {
	if execution, ok := ctx.Value(researchExecutionKey{}).(*researchExecution); ok {
		execution.identity = identity
	}
	return ctx
}
func researchHash(value any) string {
	encoded, _ := json.Marshal(value)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}
func beginResearchCheckpoint(ctx context.Context, request ResearchRequest, model string, snapshot *ResearchSnapshot) (*ResearchCheckpoint, error) {
	execution, ok := ctx.Value(researchExecutionKey{}).(*researchExecution)
	if !ok {
		return nil, nil
	}
	identity := execution.identity
	if identity == "" {
		identity = model
	}
	cp := execution.checkpoint
	if cp != nil {
		if cp.PromptVersion != ResearchPromptVersion || cp.CompressionVersion != ResearchCompressionVersion || cp.ModelIdentity != identity || researchHash(cp.Request) != researchHash(request) || cp.SnapshotHash != researchHash(snapshot) {
			return nil, fmt.Errorf("模型、思考设置、研究请求或证据已变化，请重新分析，不能继续旧研究")
		}
		return cp, nil
	}
	cp = &ResearchCheckpoint{PromptVersion: ResearchPromptVersion, CompressionVersion: ResearchCompressionVersion, ModelIdentity: identity, Request: request, SnapshotHash: researchHash(snapshot), Outputs: map[string]ResearchStageOutput{}}
	execution.checkpoint = cp
	return cp, saveResearchCheckpoint(ctx, cp, snapshot)
}
func saveResearchCheckpoint(ctx context.Context, cp *ResearchCheckpoint, snapshot *ResearchSnapshot) error {
	if cp == nil {
		return nil
	}
	cp.SnapshotHash = researchHash(snapshot)
	execution, _ := ctx.Value(researchExecutionKey{}).(*researchExecution)
	if execution == nil || execution.save == nil {
		return nil
	}
	return execution.save(cp)
}

func (cp *ResearchCheckpoint) resumable() bool {
	return cp != nil && cp.PromptVersion == ResearchPromptVersion && cp.CompressionVersion == ResearchCompressionVersion && (cp.Outline != nil || len(cp.Outputs) > 0)
}

func promptResearchJSON[T any](ctx context.Context, prompter agent.Prompter, prompt, label string, options promptJSONObjectOptions, cp *ResearchCheckpoint, snapshot *ResearchSnapshot) (T, error) {
	var decoded T
	options.validate = func(value any) error { return validateResearchStage(value, *snapshot, label) }
	var err error
	if cp != nil {
		if output, ok := cp.Outputs[label]; ok && output.PromptHash == researchHash(prompt) {
			cacheErr := json.Unmarshal(output.Value, &decoded)
			if cacheErr == nil {
				cacheErr = options.validate(&decoded)
			}
			if cacheErr == nil {
				return decoded, nil
			}
			// Old code could cache an empty core as a successful stage. Do not
			// reuse its downstream trade output or silently restart its budget.
			err = &invalidJSONResponseError{label: label, cause: fmt.Errorf("已保存%s无效：%w", label, cacheErr), content: runtimelog.Redact(truncateExactText(string(output.Value), 8_000))}
			delete(cp.Outputs, label)
			if label == "核心判断" || label == "核心判断修复" {
				for _, dependent := range []string{"核心判断修复", "交易条件", "交易条件修复", "研究结构修复"} {
					delete(cp.Outputs, dependent)
				}
			}
			decoded = *new(T)
		}
		original := options.onAttempt
		options.onAttempt = func(a promptJSONAttempt) {
			cp.Attempts = append(cp.Attempts, ResearchAttempt{Stage: researchCheckpointStage(label), DurationMS: a.durationMS, PromptBytes: a.promptBytes, ResponseBytes: a.responseBytes, Error: a.err, Progress: a.progress})
			if original != nil {
				original(a)
			}
		}
	}
	if err == nil {
		decoded, err = promptJSONObjectWithOptions[T](ctx, prompter, prompt, label, options)
	}
	if cp != nil && err != nil {
		var invalid *invalidJSONResponseError
		if errors.As(err, &invalid) {
			if cp.Failures == nil {
				cp.Failures = map[string]ResearchStageFailure{}
			}
			cp.Failures[label] = ResearchStageFailure{PromptHash: researchHash(prompt), Error: invalid.cause.Error(), Content: invalid.content}
		}
	}
	if cp != nil && err == nil {
		value, marshalErr := json.Marshal(decoded)
		if marshalErr != nil {
			return decoded, marshalErr
		}
		if cp.Outputs == nil {
			cp.Outputs = map[string]ResearchStageOutput{}
		}
		cp.Outputs[label] = ResearchStageOutput{PromptHash: researchHash(prompt), Value: value}
		delete(cp.Failures, label)
	}
	if saveErr := saveResearchCheckpoint(ctx, cp, snapshot); saveErr != nil {
		return decoded, fmt.Errorf("保存研究阶段失败：%w", saveErr)
	}
	return decoded, err
}

func researchCheckpointStage(label string) string {
	switch label {
	case "研究问题":
		return "outline"
	case "核心判断":
		return "core"
	case "交易条件":
		return "trade"
	case "快速研判":
		return "quick"
	default:
		return "repair"
	}
}

func cacheResearchValue(cp *ResearchCheckpoint, label, prompt string, value any) {
	encoded, err := json.Marshal(value)
	if err == nil {
		cp.Outputs[label] = ResearchStageOutput{PromptHash: researchHash(prompt), Value: encoded}
		delete(cp.Failures, label)
	}
}
