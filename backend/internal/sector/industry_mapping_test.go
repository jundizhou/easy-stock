package sector

import "testing"

func TestRadarIndustryMappingsCoverTencentIndustryCatalog(t *testing.T) {
	if len(radarIndustryMappings) != 124 {
		t.Fatalf("mapping count=%d want=124", len(radarIndustryMappings))
	}
	tencentCodes := make(map[string]struct{}, len(radarIndustryMappings))
	eastMoneyCodes := make(map[string]struct{}, len(radarIndustryMappings))
	for _, mapping := range radarIndustryMappings {
		if mapping.TencentCode == "" || mapping.TencentName == "" || mapping.EastMoneyBoardCode == "" || mapping.EastMoneyBoardName == "" {
			t.Fatalf("incomplete industry mapping: %+v", mapping)
		}
		if _, exists := tencentCodes[mapping.TencentCode]; exists {
			t.Fatalf("duplicate Tencent industry code: %s", mapping.TencentCode)
		}
		if _, exists := eastMoneyCodes[mapping.EastMoneyBoardCode]; exists {
			t.Fatalf("duplicate EastMoney industry board: %s", mapping.EastMoneyBoardCode)
		}
		tencentCodes[mapping.TencentCode] = struct{}{}
		eastMoneyCodes[mapping.EastMoneyBoardCode] = struct{}{}
	}
}

func TestRadarIndustryThemeUsesProviderCodeCrosswalk(t *testing.T) {
	themeID := radarIndustryThemeID("pt01801039", "非金属材料Ⅱ")
	theme, ok := FindTheme(themeID)
	if !ok || len(theme.Groups) != 1 || len(theme.Groups[0].Nodes) != 1 {
		t.Fatalf("industry theme not buildable: %+v", theme)
	}
	node := theme.Groups[0].Nodes[0]
	if node.BoardCode != "BK1020" || node.Industry != "非金属材料Ⅱ" {
		t.Fatalf("unexpected crosswalk result: %+v", node)
	}

	bankTheme, ok := FindTheme(radarIndustryThemeID("pt01801785", "农商行Ⅱ"))
	if !ok || bankTheme.Groups[0].Nodes[0].BoardCode != "BK1612" {
		t.Fatalf("level-II to level-III crosswalk missing: %+v", bankTheme)
	}
}
