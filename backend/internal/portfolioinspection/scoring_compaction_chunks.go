package portfolioinspection

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Every record remains valid JSON with its field path and source metadata.
// A long string may span records, but its claim/source identity travels with it.
type compactionRecord struct {
	Path    string         `json:"path"`
	Value   any            `json:"value"`
	Context map[string]any `json:"context,omitempty"`
	Part    int            `json:"part,omitempty"`
	Parts   int            `json:"parts,omitempty"`
}

type compactionChunk struct {
	Version        string             `json:"version"`
	Symbol         string             `json:"symbol"`
	ReportID       string             `json:"report_id"`
	InstrumentType string             `json:"instrument_type"`
	CutoffAt       time.Time          `json:"cutoff_at"`
	Horizon        string             `json:"original_horizon"`
	EvidenceLevel  string             `json:"evidence_level"`
	Merging        bool               `json:"fragment_is_summary"`
	Records        []compactionRecord `json:"records"`
}

func compactionChunks(card PortfolioAssetCard, value any, merging bool) ([]compactionChunk, error) {
	// Normalize structs without losing exact numeric JSON lexemes.
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var root any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return nil, err
	}
	base := compactionChunk{Version: compactionVersion, Symbol: card.Symbol, ReportID: card.ReportID, InstrumentType: card.Type, CutoffAt: card.CutoffAt, Horizon: card.OriginalHorizon, EvidenceLevel: card.EvidenceLevel, Merging: merging}
	fits := func(records []compactionRecord) bool {
		candidate := base
		candidate.Records = records
		encoded, err := json.Marshal(candidate)
		return err == nil && len(encoded) <= compactionChunkBytes
	}
	var records []compactionRecord
	var split func(string, any, map[string]any) error
	split = func(path string, value any, inherited map[string]any) error {
		record := compactionRecord{Path: path, Value: value, Context: inherited}
		if fits([]compactionRecord{record}) {
			records = append(records, record)
			return nil
		}
		metadataOnly := record
		metadataOnly.Value = nil
		if !fits([]compactionRecord{metadataOnly}) {
			return fmt.Errorf("组合研究的单条来源标识过长，无法安全分片")
		}
		switch v := value.(type) {
		case map[string]any:
			metadata := make(map[string]any, len(inherited)+4)
			for k, x := range inherited {
				metadata[k] = x
			}
			for _, key := range []string{"id", "kind", "source_id", "source_ids"} {
				if x, ok := v[key]; ok {
					metadata[path+"."+key] = x
				}
			}
			keys := make([]string, 0, len(v))
			for key := range v {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				if err := split(path+"."+key, v[key], metadata); err != nil {
					return err
				}
			}
		case []any:
			for i, x := range v {
				if err := split(fmt.Sprintf("%s[%d]", path, i), x, inherited); err != nil {
					return err
				}
			}
		case string:
			// Size encoded strings, not raw UTF-8: quotes, control characters and
			// HTML escaping can multiply the request size. Prefer sentence ends.
			var parts []string
			for runes := []rune(v); len(runes) > 0; {
				lo, hi := 0, min(len(runes), compactionChunkBytes)
				for lo < hi {
					mid := (lo + hi + 1) / 2
					r := record
					r.Value, r.Part, r.Parts = string(runes[:mid]), len(v), len(v)
					if fits([]compactionRecord{r}) {
						lo = mid
					} else {
						hi = mid - 1
					}
				}
				if lo == 0 {
					return fmt.Errorf("组合研究的单条来源标识过长，无法安全分片")
				}
				n := lo
				if n < len(runes) {
					for i := n - 1; i >= n/2; i-- {
						if strings.ContainsRune("。！？；\n", runes[i]) {
							n = i + 1
							break
						}
					}
				}
				part := string(runes[:n])
				parts = append(parts, part)
				runes = runes[n:]
			}
			for i, part := range parts {
				r := record
				r.Value, r.Part, r.Parts = part, i+1, len(parts)
				records = append(records, r)
			}
		default:
			return fmt.Errorf("组合研究的单条结构化资料超过分片预算")
		}
		return nil
	}
	if err := split("research", root, nil); err != nil {
		return nil, err
	}
	var chunks []compactionChunk
	current := base
	for _, record := range records {
		trial := append(append([]compactionRecord(nil), current.Records...), record)
		if !fits(trial) && len(current.Records) > 0 {
			chunks = append(chunks, current)
			current = base
		}
		current.Records = append(current.Records, record)
	}
	if len(current.Records) > 0 {
		chunks = append(chunks, current)
	}
	return chunks, nil
}

func validCompactionSummary(summary string) bool {
	if strings.TrimSpace(summary) == "" || !utf8.ValidString(summary) {
		return false
	}
	encoded, err := json.Marshal(summary)
	return err == nil && len(encoded) <= compactionSummaryBytes
}
