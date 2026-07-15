package inspector

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"
)

const BatchSchemaVersion = 2

// ReleaseNoteRule matches a chart and describes its upstream release tags.
type ReleaseNoteRule struct {
	Chart       string `yaml:"chart"`
	Provider    string `yaml:"provider"`
	Repository  string `yaml:"repository"`
	TagTemplate string `yaml:"tag_template"`
	TagSource   string `yaml:"tag_source"`
}

func chartName(chart string) string {
	return strings.TrimSuffix(chart[strings.LastIndex(chart, "/")+1:], "/")
}

// BatchManifest declares chart inspections without coupling them to a deployment system.
type BatchManifest struct {
	Rules  []ReleaseNoteRule `yaml:"rules"`
	Charts []BatchChart      `yaml:"charts"`
}

// RuleForChart returns the first matching rule for a Helm chart reference.
func (m BatchManifest) RuleForChart(chart string) ReleaseNoteRule {
	name := chartName(chart)
	for _, rule := range m.Rules {
		if rule.Chart == name {
			return rule
		}
	}
	return ReleaseNoteRule{}
}

// BatchChart describes one chart inspection in a batch manifest.
type BatchChart struct {
	Chart         string `yaml:"chart"`
	Repository    string `yaml:"repository"`
	Version       string `yaml:"version"`
	TargetVersion string `yaml:"target_version"`
	ValuesDiff    bool   `yaml:"values_diff"`
}

// BatchResult aggregates independent chart inspection results in manifest order.
type BatchResult struct {
	SchemaVersion int      `json:"schema_version"`
	Status        string   `json:"status"`
	Error         string   `json:"error,omitempty"`
	Results       []Result `json:"results"`
}

// LoadBatchManifest reads and validates the batch manifest structure.
func LoadBatchManifest(filename string) (BatchManifest, error) {
	file, err := os.Open(filename)
	if err != nil {
		return BatchManifest{}, fmt.Errorf("read batch manifest: %w", err)
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var manifest BatchManifest
	if err := decoder.Decode(&manifest); err != nil {
		return BatchManifest{}, fmt.Errorf("parse batch manifest: %w", err)
	}
	if len(manifest.Charts) == 0 {
		return BatchManifest{}, fmt.Errorf("batch manifest must contain at least one chart")
	}
	if err := validateRules(manifest.Rules); err != nil {
		return BatchManifest{}, err
	}
	return manifest, nil
}

// InspectBatch runs each manifest entry concurrently and aggregates their statuses.
func InspectBatch(
	ctx context.Context,
	manifest BatchManifest,
	releaseNoteLimit int,
	skipReleaseNotes bool,
	globalValuesDiff bool,
	globalGitHubClient string,
) BatchResult {
	result := BatchResult{
		SchemaVersion: BatchSchemaVersion,
		Status:        StatusCurrent,
		Results:       make([]Result, len(manifest.Charts)),
	}

	var wg sync.WaitGroup
	for i, chart := range manifest.Charts {
		wg.Add(1)
		go func(index int, c BatchChart) {
			defer wg.Done()
			result.Results[index] = Inspect(ctx, Input{
				Chart:            c.Chart,
				Repository:       c.Repository,
				Version:          c.Version,
				TargetVersion:    c.TargetVersion,
				IncludeDiff:      c.ValuesDiff || globalValuesDiff,
				ReleaseNoteLimit: releaseNoteLimit,
				ReleaseNoteRule:  manifest.RuleForChart(c.Chart),
				SkipReleaseNotes: skipReleaseNotes,
				GitHubClient:     globalGitHubClient,
			})
		}(i, chart)
	}
	wg.Wait()

	for _, chartResult := range result.Results {
		switch chartResult.Status {
		case StatusError:
			result.Status = StatusError
		case StatusUpdate:
			if result.Status == StatusCurrent {
				result.Status = StatusUpdate
			}
		}
	}

	return result
}

func validateRules(rules []ReleaseNoteRule) error {
	for _, rule := range rules {
		if rule.Chart == "" {
			return fmt.Errorf("release note rule is missing chart")
		}
		if rule.Provider != "" && rule.Provider != "github" && rule.Provider != "none" {
			return fmt.Errorf("unsupported release note provider %q", rule.Provider)
		}
		if rule.Provider == "github" && rule.Repository == "" {
			return fmt.Errorf("GitHub release note rule for %q is missing repository", rule.Chart)
		}
		if rule.Repository != "" && githubRepository(rule.Repository) == "" {
			return fmt.Errorf(
				"release note repository for %q must be a full GitHub URL",
				rule.Chart,
			)
		}
		if rule.TagSource != "" && rule.TagSource != "app_version" && rule.TagSource != "chart_version" {
			return fmt.Errorf("release note rule for %q has invalid tag_source %q (expected app_version or chart_version)", rule.Chart, rule.TagSource)
		}
	}
	return nil
}
