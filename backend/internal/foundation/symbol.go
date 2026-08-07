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
	} else if len(raw) >= 8 && (strings.HasPrefix(raw, "SH") || strings.HasPrefix(raw, "SZ") || strings.HasPrefix(raw, "BJ")) {
		market, code = raw[:2], raw[2:]
	} else {
		code = strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(input), "sh"), "sz")
		code = strings.TrimPrefix(code, "bj")
		code = strings.ToUpper(strings.TrimSpace(code))
	}

	if !digitsOnly.MatchString(code) {
		return Symbol{}, fmt.Errorf("unsupported symbol %q", input)
	}
	if market == "" {
		market = inferAStockMarket(code)
	}
	if market != "SH" && market != "SZ" && market != "BJ" {
		return Symbol{}, fmt.Errorf("unsupported market %q", market)
	}

	sinaPrefix := strings.ToLower(market)
	emMarket := "0"
	if market == "SH" {
		emMarket = "1"
	}

	return Symbol{
		Canonical:      code + "." + market,
		Sina:           sinaPrefix + code,
		EastMoneySecID: emMarket + "." + code,
		RawCode:        code,
		Market:         market,
	}, nil
}

func inferAStockMarket(code string) string {
	switch code[0] {
	case '6':
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
