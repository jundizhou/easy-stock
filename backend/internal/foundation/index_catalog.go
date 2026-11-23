package foundation

import "strings"

type MarketIndexDefinition struct {
	Symbol   Symbol // Canonical mainland security, including its exchange.
	ID       string
	SecID    string
	Name     string
	Region   string
	Market   string
	Currency string
	Core     bool
}

func mainlandIndex(id, ticker, name string, core bool) MarketIndexDefinition {
	symbol, err := NormalizeSymbol(ticker)
	if err != nil {
		// An invalid ticker in the built-in catalog is a programming error.
		panic(err)
	}
	return MarketIndexDefinition{ID: id, Symbol: symbol, SecID: symbol.EastMoneySecID, Name: name, Region: "中国", Market: "CN", Currency: "CNY", Core: core}
}

var marketIndexCatalog = []MarketIndexDefinition{
	mainlandIndex("sse", "000001.SH", "上证指数", true),
	mainlandIndex("szse", "399001.SZ", "深证成指", true),
	mainlandIndex("chinext", "399006.SZ", "创业板指", true),
	mainlandIndex("csi300", "000300.SH", "沪深300", true),
	mainlandIndex("sse50", "000016.SH", "上证50", true),
	mainlandIndex("csi1000", "000852.SH", "中证1000", true),
	mainlandIndex("star50", "000688.SH", "科创50", true),
	{ID: "hsi", SecID: "100.HSI", Name: "恒生指数", Region: "中国香港", Market: "HK", Currency: "HKD", Core: true},
	{ID: "nikkei", SecID: "100.N225", Name: "日经225", Region: "亚太", Market: "JP", Currency: "JPY"},
	{ID: "kospi", SecID: "100.KS11", Name: "韩国综合", Region: "亚太", Market: "KR", Currency: "KRW"},
	{ID: "taiwan", SecID: "100.TWII", Name: "台湾加权", Region: "亚太", Market: "TW", Currency: "TWD"},
	{ID: "dow", SecID: "100.DJIA", Name: "道琼斯", Region: "美洲", Market: "US", Currency: "USD", Core: true},
	{ID: "sp500", SecID: "100.SPX", Name: "标普500", Region: "美洲", Market: "US", Currency: "USD", Core: true},
	{ID: "nasdaq", SecID: "100.NDX", Name: "纳斯达克100", Region: "美洲", Market: "US", Currency: "USD", Core: true},
	{ID: "ftse", SecID: "100.FTSE", Name: "英国富时100", Region: "欧洲", Market: "UK", Currency: "GBP"},
	{ID: "dax", SecID: "100.GDAXI", Name: "德国DAX", Region: "欧洲", Market: "DE", Currency: "EUR"},
	{ID: "cac", SecID: "100.FCHI", Name: "法国CAC40", Region: "欧洲", Market: "FR", Currency: "EUR"},
}

func FindMarketIndex(id string) (MarketIndexDefinition, bool) {
	normalized := strings.ToLower(strings.TrimSpace(id))
	for _, item := range marketIndexCatalog {
		if item.ID == normalized {
			return item, true
		}
	}
	// 异动口径的 A 股基准指数：主板用 A 股指数而不是上证指数/深证成指，
	// 创业板用综指。只用于指数走势查询，不进 MarketIndexes 的列表。
	if item, ok := benchmarkIndexAliases[normalized]; ok {
		return item, true
	}
	return MarketIndexDefinition{}, false
}

var benchmarkIndexAliases = map[string]MarketIndexDefinition{
	"sse_a":        mainlandIndex("sse_a", "000003.SH", "上证A股指数", false),
	"szse_a":       mainlandIndex("szse_a", "399107.SZ", "深证A股指数", false),
	"chinext_comp": mainlandIndex("chinext_comp", "399102.SZ", "创业板综指", false),
	"bse50":        mainlandIndex("bse50", "899050.BJ", "北证50", false),
}

func MarketIndexDefinitions(scope string) []MarketIndexDefinition {
	var result []MarketIndexDefinition
	for _, index := range marketIndexCatalog {
		if scope != "core" || index.Core {
			result = append(result, index)
		}
	}
	return result
}
