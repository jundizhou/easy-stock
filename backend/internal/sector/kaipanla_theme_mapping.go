package sector

import (
	"encoding/base64"
	"strings"

	"easy-stock/backend/internal/narrative"
)

const radarMappedThemePrefix = "eastmoney-map:"

// radarThemeMapping is the local topic-level crosswalk from Kaipanla themes
// to EastMoney boards, industries, and concepts. Stocks are never mapped one
// by one: Mapper hydrates constituents from the mapped EastMoney keywords.
type radarThemeMapping struct {
	KaipanlaCode   string
	KaipanlaName   string
	EastMoneyName  string
	EastMoneyTerms []string
	StaticThemeID  string
}

// This catalog covers every unique Kaipanla theme shown in the rolling
// 20-trading-day table inspected on 2026-08-07 (60 themes).
var radarThemeMappings = []radarThemeMapping{
	{KaipanlaCode: "801660", KaipanlaName: "通信", EastMoneyName: "通信技术", EastMoneyTerms: []string{"通信设备", "通信服务", "5G", "光通信模块", "CPO概念", "卫星互联网"}},
	{KaipanlaCode: "801001", KaipanlaName: "芯片", EastMoneyName: "半导体芯片", StaticThemeID: "semiconductor"},
	{KaipanlaCode: "801159", KaipanlaName: "机器人概念", EastMoneyName: "人形机器人", StaticThemeID: "humanoid_robot"},
	{KaipanlaCode: "803023", KaipanlaName: "AI应用", EastMoneyName: "AI应用", EastMoneyTerms: []string{"AI应用", "AI智能体", "AIGC概念", "ChatGPT概念", "多模态AI", "大模型"}},
	{KaipanlaCode: "801220", KaipanlaName: "食品饮料", EastMoneyName: "食品饮料", EastMoneyTerms: []string{"食品饮料", "白酒", "饮料乳品", "休闲食品"}},
	{KaipanlaCode: "801346", KaipanlaName: "智能电网", EastMoneyName: "电网设备", StaticThemeID: "grid_equipment"},
	{KaipanlaCode: "801120", KaipanlaName: "电力", EastMoneyName: "电力", StaticThemeID: "electric_power"},
	{KaipanlaCode: "801878", KaipanlaName: "端侧AI", EastMoneyName: "物理AI", StaticThemeID: "physical_ai"},
	{KaipanlaCode: "801572", KaipanlaName: "中报增长", EastMoneyName: "业绩预增", EastMoneyTerms: []string{"中报增长", "中报预增", "业绩预增"}},
	{KaipanlaCode: "801045", KaipanlaName: "医药", EastMoneyName: "医疗/医药", StaticThemeID: "healthcare"},
	{KaipanlaCode: "801843", KaipanlaName: "商业航天", EastMoneyName: "航天", StaticThemeID: "aerospace"},
	{KaipanlaCode: "801074", KaipanlaName: "核电", EastMoneyName: "核电", EastMoneyTerms: []string{"核电", "核能核电", "核电设备"}},
	{KaipanlaCode: "801807", KaipanlaName: "算力", EastMoneyName: "AI算力", EastMoneyTerms: []string{"算力概念", "东数西算", "数据中心", "云计算", "AI服务器", "液冷服务器"}},
	{KaipanlaCode: "801199", KaipanlaName: "汽车零部件", EastMoneyName: "汽车零部件", EastMoneyTerms: []string{"汽车零部件", "汽车零部件概念"}},
	{KaipanlaCode: "801250", KaipanlaName: "并购重组", EastMoneyName: "并购重组", EastMoneyTerms: []string{"并购重组", "资产重组", "重组概念"}},
	{KaipanlaCode: "801004", KaipanlaName: "锂电池", EastMoneyName: "电池", StaticThemeID: "battery"},
	{KaipanlaCode: "801088", KaipanlaName: "有色金属", EastMoneyName: "有色金属", StaticThemeID: "nonferrous_metals"},
	{KaipanlaCode: "801027", KaipanlaName: "银行", EastMoneyName: "银行", EastMoneyTerms: []string{"银行", "银行行业"}},
	{KaipanlaCode: "801612", KaipanlaName: "脑机接口", EastMoneyName: "脑机接口", EastMoneyTerms: []string{"脑机接口"}},
	{KaipanlaCode: "801062", KaipanlaName: "军工", EastMoneyName: "国防军工", EastMoneyTerms: []string{"国防军工", "军工", "军工信息化"}},
	{KaipanlaCode: "801080", KaipanlaName: "煤炭", EastMoneyName: "煤炭", EastMoneyTerms: []string{"煤炭", "煤炭行业", "煤化工"}},
	{KaipanlaCode: "801235", KaipanlaName: "化工", EastMoneyName: "化工", StaticThemeID: "chemical"},
	{KaipanlaCode: "801314", KaipanlaName: "ST板块", EastMoneyName: "ST板块", EastMoneyTerms: []string{"ST板块", "ST股"}},
	{KaipanlaCode: "801082", KaipanlaName: "ST摘帽", EastMoneyName: "摘帽", EastMoneyTerms: []string{"ST摘帽", "摘帽"}},
	{KaipanlaCode: "801273", KaipanlaName: "股权转让", EastMoneyName: "股权转让", EastMoneyTerms: []string{"股权转让"}},
	{KaipanlaCode: "801071", KaipanlaName: "保险", EastMoneyName: "保险", EastMoneyTerms: []string{"保险", "保险行业"}},
	{KaipanlaCode: "801322", KaipanlaName: "芬太尼替代", EastMoneyName: "芬太尼", EastMoneyTerms: []string{"芬太尼替代", "芬太尼"}},
	{KaipanlaCode: "801787", KaipanlaName: "实控人变更", EastMoneyName: "控制权变更/股权转让", EastMoneyTerms: []string{"实控人变更", "控制权变更", "股权转让", "并购重组"}},
	{KaipanlaCode: "803029", KaipanlaName: "物理AI", EastMoneyName: "物理AI", StaticThemeID: "physical_ai"},
	{KaipanlaCode: "801048", KaipanlaName: "黄金", EastMoneyName: "黄金", EastMoneyTerms: []string{"黄金", "黄金概念", "贵金属"}},
	{KaipanlaCode: "801057", KaipanlaName: "石油石化", EastMoneyName: "石油石化", EastMoneyTerms: []string{"石油石化", "油气开采", "石油行业"}},
	{KaipanlaCode: "801117", KaipanlaName: "港口", EastMoneyName: "港口", EastMoneyTerms: []string{"港口", "港口航运"}},
	{KaipanlaCode: "801035", KaipanlaName: "酿酒", EastMoneyName: "酿酒", EastMoneyTerms: []string{"酿酒行业", "白酒", "啤酒概念"}},
	{KaipanlaCode: "801085", KaipanlaName: "人工智能", EastMoneyName: "人工智能", EastMoneyTerms: []string{"人工智能", "AI智能体", "AIGC概念", "大模型"}},
	{KaipanlaCode: "801445", KaipanlaName: "元器件", EastMoneyName: "电子元器件", EastMoneyTerms: []string{"电子元件", "元器件", "被动元件"}},
	{KaipanlaCode: "801733", KaipanlaName: "中特估", EastMoneyName: "中特估", EastMoneyTerms: []string{"中特估", "央企改革", "国企改革"}},
	{KaipanlaCode: "801081", KaipanlaName: "证券", EastMoneyName: "证券", EastMoneyTerms: []string{"证券", "券商概念"}},
	{KaipanlaCode: "801973", KaipanlaName: "保健品", EastMoneyName: "保健品", EastMoneyTerms: []string{"保健品", "保健食品"}},
	{KaipanlaCode: "801248", KaipanlaName: "智能驾驶", EastMoneyName: "智能驾驶", EastMoneyTerms: []string{"智能驾驶", "无人驾驶", "车联网"}},
	{KaipanlaCode: "801184", KaipanlaName: "包装印刷", EastMoneyName: "包装印刷", EastMoneyTerms: []string{"包装印刷"}},
	{KaipanlaCode: "801031", KaipanlaName: "文化传媒", EastMoneyName: "文化传媒", EastMoneyTerms: []string{"文化传媒", "影视概念", "短剧互动游戏"}},
	{KaipanlaCode: "801653", KaipanlaName: "霍乱概念", EastMoneyName: "霍乱概念", EastMoneyTerms: []string{"霍乱概念", "霍乱"}},
	{KaipanlaCode: "801095", KaipanlaName: "游戏", EastMoneyName: "游戏", EastMoneyTerms: []string{"游戏", "网络游戏", "手游概念"}},
	{KaipanlaCode: "801676", KaipanlaName: "地产链", EastMoneyName: "地产链", EastMoneyTerms: []string{"房地产", "地产链", "建筑材料", "家居用品"}},
	{KaipanlaCode: "801694", KaipanlaName: "非金属材料", EastMoneyName: "非金属材料", EastMoneyTerms: []string{"非金属材料", "玻璃玻纤", "水泥建材"}},
	{KaipanlaCode: "801254", KaipanlaName: "防脱发", EastMoneyName: "防脱发", EastMoneyTerms: []string{"防脱发"}},
	{KaipanlaCode: "801414", KaipanlaName: "教育", EastMoneyName: "教育", EastMoneyTerms: []string{"教育", "在线教育", "职业教育"}},
	{KaipanlaCode: "801040", KaipanlaName: "造纸", EastMoneyName: "造纸", EastMoneyTerms: []string{"造纸", "造纸印刷"}},
	{KaipanlaCode: "801137", KaipanlaName: "醋酸", EastMoneyName: "醋酸", EastMoneyTerms: []string{"醋酸", "化工原料"}},
	{KaipanlaCode: "801862", KaipanlaName: "高股息精选", EastMoneyName: "高股息", EastMoneyTerms: []string{"高股息精选", "高股息", "中特估"}},
	{KaipanlaCode: "801123", KaipanlaName: "服装家纺", EastMoneyName: "服装家纺", EastMoneyTerms: []string{"服装家纺", "纺织服装"}},
	{KaipanlaCode: "801856", KaipanlaName: "破净股概念", EastMoneyName: "破净股", EastMoneyTerms: []string{"破净股概念", "破净股"}},
	{KaipanlaCode: "801464", KaipanlaName: "农业", EastMoneyName: "农业", EastMoneyTerms: []string{"农业", "农牧饲渔", "种植业"}},
	{KaipanlaCode: "801373", KaipanlaName: "ETC", EastMoneyName: "ETC", EastMoneyTerms: []string{"ETC", "车联网"}},
	{KaipanlaCode: "801033", KaipanlaName: "国有企业", EastMoneyName: "国企改革", EastMoneyTerms: []string{"国有企业", "国企改革", "央企改革"}},
	{KaipanlaCode: "801631", KaipanlaName: "次新股", EastMoneyName: "次新股", EastMoneyTerms: []string{"次新股", "注册制次新股"}},
	{KaipanlaCode: "801313", KaipanlaName: "金融概念", EastMoneyName: "多元金融", EastMoneyTerms: []string{"金融概念", "多元金融", "互联网金融"}},
	{KaipanlaCode: "801234", KaipanlaName: "航运", EastMoneyName: "航运", EastMoneyTerms: []string{"航运", "港口航运"}},
	{KaipanlaCode: "801595", KaipanlaName: "北交所", EastMoneyName: "北交所", EastMoneyTerms: []string{"北交所", "北交所概念"}},
	{KaipanlaCode: "801443", KaipanlaName: "转基因", EastMoneyName: "转基因", EastMoneyTerms: []string{"转基因", "农业种植"}},
}

var radarThemeMappingsByName, radarThemeMappingsByCode, radarThemeMappingsByID = indexRadarThemeMappings(radarThemeMappings)

func indexRadarThemeMappings(items []radarThemeMapping) (map[string]radarThemeMapping, map[string]radarThemeMapping, map[string]radarThemeMapping) {
	byName := make(map[string]radarThemeMapping, len(items))
	byCode := make(map[string]radarThemeMapping, len(items))
	byID := make(map[string]radarThemeMapping, len(items))
	for _, item := range items {
		byName[normalizeThemeName(item.KaipanlaName)] = item
		byCode[item.KaipanlaCode] = item
		if item.StaticThemeID == "" {
			byID[radarMappedThemeID(item)] = item
		}
	}
	return byName, byCode, byID
}

func lookupRadarThemeMapping(code string, name string) (radarThemeMapping, bool) {
	if mapping, exists := radarThemeMappingsByCode[strings.TrimSpace(code)]; exists {
		return mapping, true
	}
	mapping, exists := radarThemeMappingsByName[normalizeThemeName(name)]
	return mapping, exists
}

func mappedFallbackThemeID(code string, name string) (string, string) {
	if mapping, exists := lookupRadarThemeMapping(code, name); exists {
		if mapping.StaticThemeID != "" {
			return mapping.StaticThemeID, mapping.EastMoneyName
		}
		return radarMappedThemeID(mapping), mapping.EastMoneyName
	}
	return narrative.ThemeID(name), name
}

func radarMappedThemeID(mapping radarThemeMapping) string {
	key := strings.TrimSpace(mapping.KaipanlaCode)
	if key == "" {
		key = base64.RawURLEncoding.EncodeToString([]byte(mapping.KaipanlaName))
	}
	return radarMappedThemePrefix + key
}

func findRadarMappedTheme(id string) (Theme, bool) {
	mapping, exists := radarThemeMappingsByID[strings.TrimSpace(id)]
	if !exists {
		return Theme{}, false
	}
	terms := append([]string(nil), mapping.EastMoneyTerms...)
	if len(terms) == 0 {
		terms = []string{mapping.EastMoneyName}
	}
	return Theme{
		ID:   id,
		Name: mapping.EastMoneyName,
		Tabs: []string{mapping.EastMoneyName},
		Groups: []Group{{
			ID:   "eastmoney_mapping",
			Name: "东财题材映射",
			Nodes: []Node{{
				ID:            "eastmoney_mapping_core",
				Name:          mapping.EastMoneyName,
				Description:   "依据本地开盘啦与东方财富题材对照表获取东财板块、行业及概念成分股。",
				BoardKeywords: terms,
			}},
		}},
	}, true
}
