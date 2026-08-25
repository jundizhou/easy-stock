package sector

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

const (
	radarIndustryThemePrefix = "industry:"
	radarFusionThemePrefix   = "fusion:"
)

type radarIndustryThemeRef struct {
	Code string `json:"c,omitempty"`
	Name string `json:"n"`
}

type radarFusionThemeRef struct {
	KaipanlaCode string `json:"k"`
	IndustryCode string `json:"c,omitempty"`
	IndustryName string `json:"n"`
}

func radarIndustryThemeID(code string, name string) string {
	return encodeRadarThemeRef(radarIndustryThemePrefix, radarIndustryThemeRef{
		Code: strings.TrimSpace(code),
		Name: strings.TrimSpace(name),
	})
}

func parseRadarIndustryThemeID(id string) (radarIndustryThemeRef, bool) {
	var ref radarIndustryThemeRef
	if !decodeRadarThemeRef(id, radarIndustryThemePrefix, &ref) {
		return radarIndustryThemeRef{}, false
	}
	ref.Code = strings.TrimSpace(ref.Code)
	ref.Name = strings.TrimSpace(ref.Name)
	return ref, ref.Name != ""
}

func radarFusionThemeID(kaipanlaCode string, industry radarIndustryThemeRef) string {
	return encodeRadarThemeRef(radarFusionThemePrefix, radarFusionThemeRef{
		KaipanlaCode: strings.TrimSpace(kaipanlaCode),
		IndustryCode: strings.TrimSpace(industry.Code),
		IndustryName: strings.TrimSpace(industry.Name),
	})
}

func parseRadarFusionThemeID(id string) (radarFusionThemeRef, bool) {
	var ref radarFusionThemeRef
	if !decodeRadarThemeRef(id, radarFusionThemePrefix, &ref) {
		return radarFusionThemeRef{}, false
	}
	ref.KaipanlaCode = strings.TrimSpace(ref.KaipanlaCode)
	ref.IndustryCode = strings.TrimSpace(ref.IndustryCode)
	ref.IndustryName = strings.TrimSpace(ref.IndustryName)
	return ref, ref.KaipanlaCode != "" && ref.IndustryName != ""
}

func encodeRadarThemeRef(prefix string, value any) string {
	payload, _ := json.Marshal(value)
	return prefix + base64.RawURLEncoding.EncodeToString(payload)
}

func decodeRadarThemeRef(id string, prefix string, target any) bool {
	if !strings.HasPrefix(id, prefix) {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, prefix))
	if err != nil || len(payload) == 0 {
		return false
	}
	return json.Unmarshal(payload, target) == nil
}
