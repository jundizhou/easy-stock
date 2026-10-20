package themelibrary

import (
	"encoding/json"
	"testing"
)

func TestRawThemeStocksAcceptsNullAndArray(t *testing.T) {
	var theme RawTheme
	if err := json.Unmarshal([]byte(`{"stocks":null}`), &theme); err != nil {
		t.Fatalf("decode null stocks: %v", err)
	}
	if len(theme.Stocks) != 0 {
		t.Fatalf("null stocks decoded as %d stocks", len(theme.Stocks))
	}

	if err := json.Unmarshal([]byte(`{"stocks":[{"code":"000938","name":"紫光股份","price":25.6}]}`), &theme); err != nil {
		t.Fatalf("decode stock array: %v", err)
	}
	if len(theme.Stocks) != 1 || theme.Stocks[0].Code != "000938" || theme.Stocks[0].Price != 25.6 {
		t.Fatalf("unexpected stocks: %+v", theme.Stocks)
	}
}
