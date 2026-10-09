package stockanalysis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
)

// [ES_REPAIR_SEMANTICS_PATCH] §3 失败归因与修复审计。
//
// §3 原文要求：
//   - 区分正常结束 / 长度截断 / 超时 / 流中断；
//   - 不能通过补齐括号把截断输出标为成功；
//   - 记录修复动作、位置、原始/修复后 hash。
//
// 本文件把「为什么这次模型输出不可用」显式建模，避免所有原因都被压成
// 同一句 "JSON object does not contain expected fields"（正是现场误导排查的根因）。

// jsonFailureClass 描述一次模型输出不可用的**根本原因**。
type jsonFailureClass string

const (
	// jsonFailureNone：没有失败（正常结束且解码成功）。
	jsonFailureNone jsonFailureClass = "none"
	// jsonFailureEmpty：空响应或仅空白。
	jsonFailureEmpty jsonFailureClass = "empty_response"
	// jsonFailureTruncated：长度截断——输出在不完整处硬停。
	// 判据：容器未闭合 / 字符串未闭合 / 尾部是残缺标量 token。
	// **不得**通过补齐括号把它标为成功（§3 明令）。
	jsonFailureTruncated jsonFailureClass = "length_truncated"
	// jsonFailureTimeout：调用层超时（context deadline / 传输超时）。
	jsonFailureTimeout jsonFailureClass = "timeout"
	// jsonFailureStreamInterrupted：流式传输中断（连接被重置 / 传输意外结束）。
	jsonFailureStreamInterrupted jsonFailureClass = "stream_interrupted"
	// jsonFailureAmbiguous：结构可配平但字段归属存在二义，无法安全自动修复。
	jsonFailureAmbiguous jsonFailureClass = "ambiguous_ownership"
	// jsonFailureMultipleRoots：出现多个根候选。
	jsonFailureMultipleRoots jsonFailureClass = "multiple_roots"
	// jsonFailureDuplicateKeys：合法 JSON 但对象内出现重复键。
	jsonFailureDuplicateKeys jsonFailureClass = "duplicate_keys"
	// jsonFailureSchema：结构合法但不符合任务字段契约。
	jsonFailureSchema jsonFailureClass = "schema_mismatch"
	// jsonFailureUnknown：结构合法、原因不明。
	jsonFailureUnknown jsonFailureClass = "unknown"
)

// jsonStructuralDefectClass 把结构分析结果映射为失败类别。
func jsonStructuralDefectClass(kind jsonStructureKind) jsonFailureClass {
	switch kind {
	case jsonStructureAmbiguous:
		return jsonFailureAmbiguous
	case jsonStructureTruncated:
		return jsonFailureTruncated
	case jsonStructureMultipleRt:
		return jsonFailureMultipleRoots
	default:
		return jsonFailureUnknown
	}
}

// classifyTransportFailure 把调用层错误翻译成传输级失败类别。
//
// 只做「关键词/接口」层面的事实判定，不猜测模型意图：
//   - context.DeadlineExceeded / net.Error.Timeout() → timeout
//   - unexpected EOF / connection reset / broken pipe / stream closed → stream_interrupted
func classifyTransportFailure(err error) jsonFailureClass {
	if err == nil {
		return jsonFailureNone
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return jsonFailureTimeout
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return jsonFailureTimeout
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "deadline exceeded"),
		strings.Contains(msg, "timeout"),
		strings.Contains(msg, "timed out"):
		return jsonFailureTimeout
	case strings.Contains(msg, "unexpected eof"),
		strings.Contains(msg, "connection reset"),
		strings.Contains(msg, "broken pipe"),
		strings.Contains(msg, "stream closed"),
		strings.Contains(msg, "eof"):
		return jsonFailureStreamInterrupted
	}
	return jsonFailureUnknown
}

// classifyDecodeFailure 在「结构修复判定」的基础上给出本次不可用的根本原因。
//
// 关键约束（§3）：截断**必须**显式识别，绝不能因为「补齐括号能解析」就报成功。
// 因此这里先做结构分析，把 truncated / ambiguous / multiple_roots 排在契约判定之前。
func classifyDecodeFailure(content string, decodeErr error) jsonFailureClass {
	if strings.TrimSpace(content) == "" {
		return jsonFailureEmpty
	}
	_, _, kind := sanitizeJSONStructureAnalyze(content)
	switch kind {
	case jsonStructureTruncated:
		return jsonFailureTruncated
	case jsonStructureAmbiguous:
		return jsonFailureAmbiguous
	case jsonStructureMultipleRt:
		return jsonFailureMultipleRoots
	}
	if hasDuplicateJSONKeys(content) {
		return jsonFailureDuplicateKeys
	}
	if decodeErr != nil {
		msg := decodeErr.Error()
		if strings.Contains(msg, "does not contain expected fields") ||
			strings.Contains(msg, "cannot unmarshal") ||
			strings.Contains(msg, "unknown field") {
			return jsonFailureSchema
		}
	}
	return jsonFailureUnknown
}

// analyzeStructureKind 只取结构分析的类型判定，便于解码路径前置拒绝截断。
func analyzeStructureKind(raw string) jsonStructureKind {
	_, _, kind := sanitizeJSONStructureAnalyze(raw)
	return kind
}

// truncatedJSONResponseError 表示模型输出在完整 JSON 之前被截断（长度上限/流中断）。
// §3：不得补齐括号或截取片段把它标为成功；调用方应据此重新生成。
type truncatedJSONResponseError struct {
	raw string
}

func (e *truncatedJSONResponseError) Error() string {
	return fmt.Sprintf("Hermes输出在JSON完整前被截断（长度上限或流中断），已拒绝而非补齐括号：尾部 %q",
		tailSnippet(e.raw, 60))
}

func tailSnippet(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// repairAudit 是一次解码尝试的审计记录（§3：记录修复动作、位置、原始/修复后 hash）。
type repairAudit struct {
	Class           jsonFailureClass   `json:"class"`
	Repaired        bool               `json:"repaired"`
	RawSHA256       string             `json:"raw_sha256"`
	SanitizedSHA256 string             `json:"sanitized_sha256"`
	Actions         []jsonRepairAction `json:"actions,omitempty"`
}

// newRepairAudit 计算原始/修复后 hash 与动作清单。
func newRepairAudit(raw, sanitized string, actions []jsonRepairAction, class jsonFailureClass) repairAudit {
	return repairAudit{
		Class:           class,
		Repaired:        len(actions) > 0,
		RawSHA256:       sha256Hex(raw),
		SanitizedSHA256: sha256Hex(sanitized),
		Actions:         actions,
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
