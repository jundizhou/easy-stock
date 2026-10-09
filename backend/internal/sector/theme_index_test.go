package sector

import (
	"context"
	"testing"

	"easy-stock/backend/internal/providers/duanxianxia"
)

func TestFusedThemeIndexDoesNotSubstituteItsBroaderIndustry(t *testing.T) {
	provider := NewRadarProvider(fakeRadarSource{snapshot: duanxianxia.Snapshot{ID: "s", Themes: []duanxianxia.Theme{{Code: "803023", Name: "AI应用"}}}}, nil, nil, RadarProviderConfig{})
	id := radarFusionThemeID("803023", radarIndustryThemeRef{Code: "BK1034", Name: "软件开发"})
	target, err := provider.ResolveThemeIndex(context.Background(), id, "s")
	if err != nil || target.Name != "AI应用" || target.BoardCode != "" {
		t.Fatalf("topic replaced by broad industry: %+v %v", target, err)
	}
	if _, err = provider.ResolveThemeIndex(context.Background(), id, "expired"); err != ErrSnapshotExpired {
		t.Fatalf("expired snapshot: %v", err)
	}
}
