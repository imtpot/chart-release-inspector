package inspector

import (
	"fmt"
	"os"

	"go.yaml.in/yaml/v3"
)

const BatchSchemaVersion = 4

// BatchManifest declares chart inspections without coupling them to a deployment system.
type BatchManifest struct {
	Charts []BatchChart `yaml:"charts"`
}

// BatchChart describes one chart inspection in a batch manifest. Each entry is
// inspected exactly like a standalone inspect call with the global CLI flags.
type BatchChart struct {
	Chart         string `yaml:"chart"`
	Repository    string `yaml:"repository"`
	Version       string `yaml:"version"`
	TargetVersion string `yaml:"target_version"`
	AppRepository string `yaml:"app_repository"`
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
	if err := validateCharts(manifest.Charts); err != nil {
		return BatchManifest{}, err
	}
	return manifest, nil
}

func validateCharts(charts []BatchChart) error {
	for _, chart := range charts {
		if chart.AppRepository != "" && githubRepository(chart.AppRepository) == "" {
			return fmt.Errorf("app_repository for %q must be a full GitHub URL", chart.Chart)
		}
	}
	return nil
}
