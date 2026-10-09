package stockanalysis

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// [ES_PARTIAL_CORE_SAVE_PATCH] §4 受控分段保存（core-only）。
//
// 现场问题：deep 研究分三段（outline → core → trade）。旧实现里，
// **trade 段失败 = 整次研究失败**（research.go 中 trade 出错直接 return err），
// 即便 core 段已经产出并通过了完整的语义校验（来源引用、逐字引用、跨字段关系）。
// 结果是四标的实测里，模型只是晚一步把交易条件写坏，就让整份可用的研究结论一起被丢。
//
// §4 要求的边界（本文件严格实现，不越界）：
//  1. 只有**独立通过完整语义校验**的 core 才可保存；不得保存「部分通过」的 core。
//  2. 保留：已通过校验的 core、证据快照、版本 hash。
//  3. 明确显示「研究可用，交易计划未生成」；**不计入完整 succeeded**。
//  4. 不得展示未通过校验的 conditions / scenarios；不得给出可执行计划入口。
//  5. **core 的来源或逐字引用失败 → 仍然整体失败**（不做部分保存）。
//  6. 交易重试必须固定复用同一份 core / 证据快照。

// ResearchPartialStatus 描述一次分段保存的可信度。
type ResearchPartialStatus string

const (
	// ResearchPartialNone：没有任何可用产物（整体失败）。
	ResearchPartialNone ResearchPartialStatus = ""
	// ResearchPartialCoreOnly：core 已通过完整语义校验，但交易条件未生成。
	// 对用户明示「研究可用，交易计划未生成」。
	ResearchPartialCoreOnly ResearchPartialStatus = "core_only"
)

// researchPartialDeemableKeys 是「core 段自身可信」必须存在的字段集合。
// 缺任一即视为不可保存（§4.1）。
var researchPartialDeemableKeys = []string{"headline", "thesis"}

// PartialResearchError 表示「core 可用但交易段失败」的可恢复错误。
// 调用方据此走 §4 受控保存路径，而不是整单丢弃。
type PartialResearchError struct {
	Core    ResearchCoreSynthesis
	Outline ResearchOutline
	// CoreNotes 是 core 段通过校验时产生的说明（§4.2 随产物一并保留）。
	CoreNotes []string
	// TradeErr 是交易段失败的原因原文（用于展示与「交易重试」入口）。
	TradeErr error
}

func (e *PartialResearchError) Error() string {
	return fmt.Sprintf("研究可用但交易计划未生成：%v", e.TradeErr)
}

func (e *PartialResearchError) Unwrap() error { return e.TradeErr }

func isPartialResearch(err error) (*PartialResearchError, bool) {
	var partial *PartialResearchError
	if errors.As(err, &partial) {
		return partial, true
	}
	return nil, false
}

// validateCoreOnly 判定 core 段是否**独立通过完整语义校验**（§4.1）。
//
// 判据不依赖 trade 段：把 core 装进一个「无交易条件」的 ResearchSynthesis，
// 用 validateResearchCore 单独跑 —— 它执行与完整校验**完全相同**的 claim 级检查
// （来源编号存在性、基线禁引、逐字引文匹配、事实来源类型、禁语、headline 必填、
// 证据充分度归一化），但不施加交易段义务（conditions/scenarios/decision）。
//
// 若 core 自身的来源编号非法或逐字引用对不上，这里就会失败 → 不得部分保存（§4.5）。
//
// 返回 (结论说明, 是否可保存)。notes 为通过时产生的提示。
func validateCoreOnly(core ResearchCoreSynthesis, snapshot ResearchSnapshot, request ResearchRequest) ([]string, bool) {
	probe := ResearchSynthesis{
		Headline:         core.Headline,
		Thesis:           core.Thesis,
		Support:          core.Support,
		Counter:          core.Counter,
		Alternatives:     core.Alternatives,
		MainConflict:     core.MainConflict,
		EvidenceLevel:    core.EvidenceLevel,
		Limitations:      core.Limitations,
		BaselineRelation: core.BaselineRelation,
		BaselineReason:   core.BaselineReason,
		// 交易段留空——core 校验不得依赖交易条件。
		Conditions:      nil,
		InvalidationIDs: nil,
		Scenarios:       nil,
	}
	notes, err := validateResearchCore(&probe, snapshot)
	if err != nil {
		return nil, false
	}
	// 额外硬约束：core 的核心字段必须齐全（§4.1 的「完整」）。
	if !researchCoreIsComplete(core) {
		return nil, false
	}
	return notes, true
}

// researchCoreIsComplete 检查 core 是否含足以支撑「研究可用」的最小字段集。
func researchCoreIsComplete(core ResearchCoreSynthesis) bool {
	if trimResearchSpace(core.Headline) == "" {
		return false
	}
	if trimResearchSpace(core.Thesis.Text) == "" {
		return false
	}
	// 研究结论必须至少有一条可用来源支撑的论据，否则「可用」无从谈起。
	if len(core.Support) == 0 {
		return false
	}
	for _, claim := range core.Support {
		if trimResearchSpace(claim.Text) == "" || len(claim.SourceIDs) == 0 {
			return false
		}
	}
	return true
}

// buildPartialReport 构造「core-only」的 ResearchReport（§4.2/§4.3/§4.4）。
//
// 关键：交易相关字段**全部留空**，绝不展示未通过校验的 conditions / scenarios，
// 也不产生任何可执行计划（decision 置为 no_plan 语义的中性值）。
func buildPartialReport(
	core ResearchCoreSynthesis,
	snapshot ResearchSnapshot,
	request ResearchRequest,
	level ResearchLevel,
	model string,
	attempts []ResearchAttempt,
	compression ResearchPromptCompression,
	notes []string,
	coreHash string,
) ResearchReport {
	partialNotes := append([]string{}, notes...)
	partialNotes = append(partialNotes, "交易计划未生成：交易条件阶段失败，本报告仅含已通过校验的核心研究。")
	return ResearchReport{
		Runtime: "partial-core-only",
		ResearchSynthesis: ResearchSynthesis{
			Headline:         core.Headline,
			Thesis:           core.Thesis,
			Support:          core.Support,
			Counter:          core.Counter,
			Alternatives:     core.Alternatives,
			MainConflict:     core.MainConflict,
			EvidenceLevel:    core.EvidenceLevel,
			Limitations:      core.Limitations,
			BaselineRelation: core.BaselineRelation,
			BaselineReason:   core.BaselineReason,
			// §4.4：不得展示未校验的交易条件与情景。
			Conditions:      nil,
			InvalidationIDs: nil,
			Scenarios:       nil,
			Decision:        ResearchDecision{}, // 无可执行计划
		},
		SnapshotID:      snapshot.ID,
		SnapshotVersion: snapshot.Version,
		PromptVersion:   ResearchPromptVersion,
		Request:         request,
		AnalysisLevel:   level,
		Model:           model,
		GeneratedAt:     time.Now().UTC(),
		CutoffAt:        snapshot.CutoffAt,
		Sources:         snapshot.Sources,
		Anchors:         snapshot.Anchors,
		Attempts:        attempts,
		Compression:     compression,
		Validation:      "core_only_pending_trade",
		ValidationNotes: partialNotes,
		Partial:         true,
		PartialStatus:   ResearchPartialCoreOnly,
		CoreHash:        coreHash,
	}
}

// applyPartialResearch 把 core-only 报告落到 Analysis，并打上
// **明确不可当作完整研究**的状态标记（§4.3）。
func applyPartialResearch(analysis *Analysis, report *ResearchReport, snapshot ResearchSnapshot) {
	analysis.ResearchReport = report
	analysis.SnapshotID = snapshot.ID
	analysis.Scorecard = snapshot.Baseline
	// §4.3：不以完整成功呈现；message 明示「研究可用，交易计划未生成」。
	analysis.AI = AISynthesisStatus{
		Status:  "partial",
		Message: "研究可用，交易计划未生成",
	}
	// §4.4：不写 conclusion/action_plan/signals —— 避免前端渲染出可执行计划入口。
	if analysis.Conclusion.Headline == "" {
		analysis.Conclusion = Conclusion{
			Headline: report.Headline,
			Summary:  report.Thesis.Text,
			Source:   "hermes-research-partial",
		}
	}
	// 风险条目来自已校验的 counter，属于 core 的一部分，可以展示。
	analysis.Risks = uniqueStrings(claimTexts(report.Counter), 8)
	if len(analysis.Risks) == 0 {
		analysis.Risks = []string{"交易计划未生成，且证据未排除潜在风险"}
	}
}

// coreSynthesisHash 给出 core 内容的稳定 hash（§4.2 版本 hash）。
func coreSynthesisHash(core ResearchCoreSynthesis) string {
	encoded, err := json.Marshal(core)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func trimResearchSpace(s string) string {
	start, end := 0, len(s)
	for start < end && isJSONSpace(s[start]) {
		start++
	}
	for end > start && isJSONSpace(s[end-1]) {
		end--
	}
	return s[start:end]
}
