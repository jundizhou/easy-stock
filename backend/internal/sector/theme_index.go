package sector

import (
	"context"
	"fmt"
	"strings"

	"easy-stock/backend/internal/foundation"
)

func (m *Mapper) ResolveThemeIndex(_ context.Context, themeID, _ string) (foundation.ThemeIndexTarget, error) {
	theme, ok := FindTheme(themeID)
	if !ok {
		return foundation.ThemeIndexTarget{}, fmt.Errorf("unknown sector theme: %s", themeID)
	}
	return foundation.ThemeIndexTarget{Name: theme.Name, Names: []string{theme.Name}}, nil
}

func (p *RadarProvider) ResolveThemeIndex(ctx context.Context, themeID, snapshotID string) (foundation.ThemeIndexTarget, error) {
	if industry, ok := parseRadarIndustryThemeID(themeID); ok {
		return foundation.ThemeIndexTarget{Name: industry.Name, Names: []string{industry.Name}, BoardCode: industry.Code}, nil
	}
	code := strings.TrimPrefix(themeID, "kpl:")
	if fusion, ok := parseRadarFusionThemeID(themeID); ok {
		// A fused topic is still the Kaipanla topic; its associated industry is
		// not necessarily a faithful index for that topic (e.g. AI vs software).
		code = fusion.KaipanlaCode
	} else if !strings.HasPrefix(themeID, "kpl:") {
		if resolver, ok := p.fallback.(interface {
			ResolveThemeIndex(context.Context, string, string) (foundation.ThemeIndexTarget, error)
		}); ok {
			return resolver.ResolveThemeIndex(ctx, themeID, snapshotID)
		}
		return foundation.ThemeIndexTarget{}, fmt.Errorf("unknown sector theme: %s", themeID)
	}
	if p.source == nil {
		return foundation.ThemeIndexTarget{}, fmt.Errorf("题材快照不可用")
	}
	snapshot, exists, err := p.source.SnapshotByID(ctx, snapshotID)
	if snapshotID == "" {
		snapshot, _, err = p.source.Snapshot(ctx)
		exists = err == nil
	}
	if err != nil {
		return foundation.ThemeIndexTarget{}, err
	}
	if !exists {
		return foundation.ThemeIndexTarget{}, ErrSnapshotExpired
	}
	for _, theme := range snapshot.Themes {
		if theme.Code != code {
			continue
		}
		target := foundation.ThemeIndexTarget{Name: theme.Name, Names: []string{theme.Name}}
		if mapping, ok := lookupRadarThemeMapping(code, theme.Name); ok && mapping.EastMoneyName != theme.Name {
			target.Names = append(target.Names, mapping.EastMoneyName)
		}
		return target, nil
	}
	return foundation.ThemeIndexTarget{}, fmt.Errorf("题材不在指定快照中")
}
