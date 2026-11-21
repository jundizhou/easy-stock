package foundation

import (
	"fmt"
	"regexp"
	"strings"
)

type Symbol struct {
	Canonical      string
	Sina           string
	EastMoneySecID string
	RawCode        string
	Market         string
}

var digitsOnly = regexp.MustCompile(`^\d{5,6}$`)

func NormalizeSymbol(input string) (Symbol, error) {
	raw := strings.ToUpper(strings.TrimSpace(input))
	if raw == "" {
		return Symbol{}, fmt.Errorf("symbol is required")
	}

	market := ""
	code := raw
	if strings.Contains(raw, ".") {
		parts := strings.Split(raw, ".")
		if len(parts) != 2 {
			return Symbol{}, fmt.Errorf("invalid symbol %q", input)
		}
		code, market = parts[0], parts[1]
	} else {
		// A 股用 SH/SZ/BJ 前缀，港股用 HK 前缀（如 hk00700）。
		// 代码本身都是数字，不会与这些字母前缀冲突。
		for _, prefix := range []string{"SH", "SZ", "BJ", "HK"} {
			if strings.HasPrefix(raw, prefix) {
				market, code = prefix, raw[len(prefix):]
				break
			}
		}
	}

	if !digitsOnly.MatchString(code) {
		return Symbol{}, fmt.Errorf("unsupported symbol %q", input)
	}
	if market == "" {
		// 5 位裸代码是港股（A 股一律 6 位），据此推断市场。
		if len(code) == 5 {
			market = "HK"
		} else {
			market = inferAStockMarket(code)
		}
	}
	if market != "SH" && market != "SZ" && market != "BJ" && market != "HK" {
		return Symbol{}, fmt.Errorf("unsupported market %q", market)
	}

	sinaPrefix := strings.ToLower(market)
	emMarket := "0"
	switch market {
	case "SH":
		emMarket = "1"
	case "HK":
		// 港股 secid 固定 116. 前缀 + 5 位零填充代码（00700.HK → 116.00700）。
		emMarket = "116"
	}

	return Symbol{
		Canonical:      code + "." + market,
		Sina:           sinaPrefix + code,
		EastMoneySecID: emMarket + "." + code,
		RawCode:        code,
		Market:         market,
	}, nil
}

// IsHongKongSymbol 判断规范化代码（00700.HK）是否为港股。
// 港股 K 线、行情与 A 股走不同数据源，调用方据此分流。
func IsHongKongSymbol(symbol string) bool {
	normalized, err := NormalizeSymbol(symbol)
	if err != nil {
		return false
	}
	return normalized.Market == "HK"
}

func inferAStockMarket(code string) string {
	switch code[0] {
	case '5', '6':
		return "SH"
	case '0', '3':
		return "SZ"
	case '4', '8', '9':
		return "BJ"
	default:
		return "SZ"
	}
}

func SplitSymbols(input string) ([]string, error) {
	parts := strings.Split(input, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		s := strings.TrimSpace(part)
		if s == "" {
			continue
		}
		n, err := NormalizeSymbol(s)
		if err != nil {
			return nil, err
		}
		out = append(out, n.Canonical)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("symbols is required")
	}
	return out, nil
}
