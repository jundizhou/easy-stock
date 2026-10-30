package portfolioinspection

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCompactionChunksKeepClaimIdentityAndExactText(t *testing.T) {
	text := strings.Repeat("需核验\"风险\"<>&🙂。\n", 10000)
	value := map[string]any{"counter": []any{map[string]any{"text": text, "kind": "inference", "source_ids": []string{"s1", "s2"}}}}
	card := PortfolioAssetCard{Symbol: "600519.SH", ReportID: "report1"}
	chunks, err := compactionChunks(card, value, false)
	if err != nil || len(chunks) < 2 {
		t.Fatalf("expected structured chunks: %v", err)
	}
	var reconstructed strings.Builder
	parts, declaredParts := 0, 0
	for _, chunk := range chunks {
		data, err := json.Marshal(chunk)
		if err != nil || len(data) > compactionChunkBytes || !json.Valid(data) || !utf8.Valid(data) {
			t.Fatalf("invalid wire chunk: %d %v", len(data), err)
		}
		if chunk.Symbol != card.Symbol || chunk.ReportID != card.ReportID {
			t.Fatal("fragment lost report identity")
		}
		for _, record := range chunk.Records {
			if record.Path != "research.counter[0].text" {
				continue
			}
			parts++
			declaredParts = record.Parts
			if record.Part != parts || record.Context["research.counter[0].kind"] != "inference" {
				t.Fatal("claim type/part lost", record.Path)
			}
			sources, _ := json.Marshal(record.Context["research.counter[0].source_ids"])
			if string(sources) != `["s1","s2"]` {
				t.Fatal("claim sources detached from text", string(sources))
			}
			reconstructed.WriteString(record.Value.(string))
		}
	}
	if reconstructed.String() != text || parts != declaredParts {
		t.Fatal("split lost or changed research text")
	}
}

func TestCompactionKeepsSmallStructuredValuesAndNumericPrecision(t *testing.T) {
	value := map[string]any{"counter": []any{map[string]any{"text": "未发生违约", "kind": "fact", "source_ids": []string{"s1"}}}, "amount": json.Number("9007199254740993")}
	chunks, err := compactionChunks(PortfolioAssetCard{Symbol: "600519.SH"}, value, false)
	if err != nil || len(chunks) != 1 || len(chunks[0].Records) != 1 {
		t.Fatal("small object unnecessarily split", err)
	}
	want, _ := json.Marshal(value)
	got, _ := json.Marshal(chunks[0].Records[0].Value)
	if string(got) != string(want) {
		t.Fatalf("claim or numeric value changed: %s", got)
	}
}

func TestCompactionRejectsUnpackableSourceMetadataWithoutDroppingText(t *testing.T) {
	value := map[string]any{"source_ids": []string{strings.Repeat("x", compactionChunkBytes)}, "text": "必须保留的反证"}
	if _, err := compactionChunks(PortfolioAssetCard{}, value, false); err == nil {
		t.Fatal("oversized provenance silently dropped")
	}
}
