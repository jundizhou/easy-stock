package foundation

// ThemeProgress is an immutable published view. Steps can finish independently;
// partial data remains useful when one of the upstreams fails.
type ThemeProgress struct {
	Data       []ThemeOverview   `json:"data"`
	Meta       SourceMeta        `json:"meta"`
	Revision   uint64            `json:"revision"`
	RefreshID  string            `json:"refresh_id"`
	Stage      string            `json:"stage"`
	Refreshing bool              `json:"refreshing"`
	Steps      map[string]string `json:"steps"`
	Errors     map[string]string `json:"errors"`
}
