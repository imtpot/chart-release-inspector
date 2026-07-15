package inspector

// Input describes a chart update to inspect without binding to an IaC tool.
type Input struct {
	Chart            string
	Repository       string
	Version          string
	TargetVersion    string
	IncludeDiff      bool
	IncludeChangelog bool
	ChangelogLimit   int
	AppRepository    string
}

const (
	StatusCurrent = "current"
	StatusUpdate  = "update_available"
	StatusError   = "error"
)

// ChangelogEntry is a bounded upstream changelog payload for automation.
type ChangelogEntry struct {
	Version        string   `json:"version"`
	URL            string   `json:"url"`
	BodyPreview    []string `json:"body_preview"`
	BodyCharacters int      `json:"body_characters"`
	Truncated      bool     `json:"truncated"`
}

// Result is the machine-readable contract emitted by the standalone CLI.
type Result struct {
	Chart              string           `json:"chart,omitempty"`
	Status             string           `json:"status"`
	SourceType         string           `json:"source_type"`
	ChartVersion       string           `json:"chart_version"`
	TargetChartVersion string           `json:"target_chart_version,omitempty"`
	AppVersion         string           `json:"app_version,omitempty"`
	TargetAppVersion   string           `json:"target_app_version,omitempty"`
	Error              string           `json:"error,omitempty"`
	ValuesDiff         []string         `json:"values_diff,omitempty"`
	ValuesDiffChanged  *bool            `json:"values_diff_changed,omitempty"`
	ValuesDiffError    string           `json:"values_diff_error,omitempty"`
	ChangelogError     string           `json:"changelog_error,omitempty"`
	Changelog          []ChangelogEntry `json:"changelog"`
}

type chartVersion struct {
	Version    string
	AppVersion string
	Source     string
	URLs       []string
	Values     []byte
}
