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
	// ChangelogGroup references an entry in BatchResult.ChangelogGroups when
	// --deduplicate collapsed this result's changelog into a shared group.
	// When set, Changelog is left empty to avoid repeating the same content.
	ChangelogGroup string `json:"changelog_group,omitempty"`
}

// ChangelogGroup holds changelog content shared by multiple chart results,
// used by --deduplicate to avoid repeating identical upstream release notes
// across charts that track the same application release.
type ChangelogGroup struct {
	ID        string           `json:"id"`
	Charts    []string         `json:"charts"`
	Changelog []ChangelogEntry `json:"changelog"`
}

type chartVersion struct {
	Version    string
	AppVersion string
	Source     string
	URLs       []string
	Values     []byte
}
