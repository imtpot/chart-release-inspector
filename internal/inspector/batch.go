package inspector

import (
	"context"
	"fmt"
	"os"

	"go.yaml.in/yaml/v3"
)

const BatchSchemaVersion = 1

// BatchManifest declares chart inspections without coupling them to a deployment system.
type BatchManifest struct {
	Charts []BatchChart `yaml:"charts"`
}

// BatchChart describes one chart inspection in a batch manifest.
type BatchChart struct {
	Chart          string `yaml:"chart"`
	Repository     string `yaml:"repository"`
	CurrentVersion string `yaml:"current_version"`
	TargetVersion  string `yaml:"target_version"`
	ValuesDiff     bool   `yaml:"values_diff"`
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
	return manifest, nil
}

// InspectBatch runs each manifest entry independently and aggregates their statuses.
func InspectBatch(
	ctx context.Context,
	manifest BatchManifest,
	releaseNotesConfig ReleaseNotesConfig,
	releaseNoteLimit int,
) BatchResult {
	result := BatchResult{
		SchemaVersion: BatchSchemaVersion,
		Status:        StatusCurrent,
		Results:       make([]Result, 0, len(manifest.Charts)),
	}
	for _, chart := range manifest.Charts {
		chartResult := Inspect(ctx, Input{
			Chart:            chart.Chart,
			Repository:       chart.Repository,
			CurrentVersion:   chart.CurrentVersion,
			TargetVersion:    chart.TargetVersion,
			IncludeDiff:      chart.ValuesDiff,
			ReleaseNoteLimit: releaseNoteLimit,
			ReleaseNoteRule:  releaseNotesConfig.RuleForChart(chart.Chart),
		})
		result.Results = append(result.Results, chartResult)
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
