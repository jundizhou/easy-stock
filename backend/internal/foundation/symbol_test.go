package foundation

import "testing"

func TestNormalizeAStockSymbol(t *testing.T) {
	tests := map[string]struct {
		wantCanonical string
		wantSina      string
		wantEM        string
	}{
		"000001.SZ": {"000001.SZ", "sz000001", "0.000001"},
		"sz000001":  {"000001.SZ", "sz000001", "0.000001"},
		"600000":    {"600000.SH", "sh600000", "1.600000"},
		"688001":    {"688001.SH", "sh688001", "1.688001"},
		"300750":    {"300750.SZ", "sz300750", "0.300750"},
		"830799":    {"830799.BJ", "bj830799", "0.830799"},
	}

	for input, tt := range tests {
		got, err := NormalizeSymbol(input)
		if err != nil {
			t.Fatalf("NormalizeSymbol(%q) returned error: %v", input, err)
		}
		if got.Canonical != tt.wantCanonical {
			t.Fatalf("NormalizeSymbol(%q).Canonical = %q, want %q", input, got.Canonical, tt.wantCanonical)
		}
		if got.Sina != tt.wantSina {
			t.Fatalf("NormalizeSymbol(%q).Sina = %q, want %q", input, got.Sina, tt.wantSina)
		}
		if got.EastMoneySecID != tt.wantEM {
			t.Fatalf("NormalizeSymbol(%q).EastMoneySecID = %q, want %q", input, got.EastMoneySecID, tt.wantEM)
		}
	}
}

func TestNormalizeSymbolRejectsEmptyInput(t *testing.T) {
	_, err := NormalizeSymbol("  ")
	if err == nil {
		t.Fatal("NormalizeSymbol(empty) error = nil, want error")
	}
}
