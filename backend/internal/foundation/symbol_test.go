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

func TestNormalizeHongKongSymbol(t *testing.T) {
	tests := map[string]struct {
		wantCanonical string
		wantSina      string
		wantEM        string
	}{
		"00700.HK": {"00700.HK", "hk00700", "116.00700"},
		"hk00700":  {"00700.HK", "hk00700", "116.00700"},
		"HK00700":  {"00700.HK", "hk00700", "116.00700"},
		"00700":    {"00700.HK", "hk00700", "116.00700"},
		"09988":    {"09988.HK", "hk09988", "116.09988"},
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

func TestIsHongKongSymbol(t *testing.T) {
	cases := map[string]bool{
		"00700.HK": true, "hk00700": true, "00700": true,
		"600519.SH": false, "000001": false, "510300": false, "": false, "00700.US": false,
	}
	for input, want := range cases {
		if got := IsHongKongSymbol(input); got != want {
			t.Fatalf("IsHongKongSymbol(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestNormalizeSymbolRejectsUnknownMarket(t *testing.T) {
	for _, input := range []string{"00700.US", "us00700", "00700.SH", "bj00700", "600519.HK", "HK600519"} {
		if _, err := NormalizeSymbol(input); err == nil {
			t.Fatalf("NormalizeSymbol(%q) error = nil, want error", input)
		}
	}
}

func TestIsListedFundCandidateExcludesHongKong(t *testing.T) {
	// 港股 5 位代码可能撞上 A 股 ETF 的 5/15/16 前缀，不能被判成基金。
	for _, symbol := range []string{"00700.HK", "05100.HK", "hk00522"} {
		if IsListedFundCandidate(symbol) {
			t.Fatalf("IsListedFundCandidate(%q) = true, want false", symbol)
		}
	}
	if !IsListedFundCandidate("510300.SH") || !IsListedFundCandidate("159915.SZ") {
		t.Fatal("A 股 ETF 仍应被识别为基金")
	}
}
