package inspector

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
	"sigs.k8s.io/kustomize/api/konfig"
	kustomizetypes "sigs.k8s.io/kustomize/api/types"
)

// LoadKustomizationManifest reads a kustomization.yaml's helmCharts entries
// and adapts them into a BatchManifest, so they can be inspected exactly
// like a hand-written batch manifest. Path may point directly at a
// kustomization file or at a directory containing one.
func LoadKustomizationManifest(path string) (BatchManifest, error) {
	resolved, err := resolveKustomizationFile(path)
	if err != nil {
		return BatchManifest{}, err
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return BatchManifest{}, fmt.Errorf("read kustomization: %w", err)
	}

	var kustomization kustomizetypes.Kustomization
	if err := yaml.Unmarshal(data, &kustomization); err != nil {
		return BatchManifest{}, fmt.Errorf("parse kustomization %q: %w", resolved, err)
	}
	if len(kustomization.HelmCharts) == 0 {
		return BatchManifest{}, fmt.Errorf("kustomization %q has no helmCharts entries", resolved)
	}

	manifest := BatchManifest{Charts: make([]BatchChart, len(kustomization.HelmCharts))}
	for i, helmChart := range kustomization.HelmCharts {
		manifest.Charts[i] = batchChartFromHelmChart(helmChart)
	}
	if err := validateCharts(manifest.Charts); err != nil {
		return BatchManifest{}, err
	}
	return manifest, nil
}

// batchChartFromHelmChart adapts a kustomize helmCharts entry into a
// BatchChart. Fields with no bearing on version inspection (namespace,
// releaseName, valuesFile, includeCRDs, ...) are intentionally dropped.
func batchChartFromHelmChart(helmChart kustomizetypes.HelmChart) BatchChart {
	chart, repository := helmChart.Name, helmChart.Repo
	if strings.HasPrefix(helmChart.Repo, "oci://") {
		chart = strings.TrimSuffix(helmChart.Repo, "/") + "/" + helmChart.Name
		repository = ""
	}
	return BatchChart{
		Chart:      chart,
		Repository: repository,
		Version:    helmChart.Version,
	}
}

// resolveKustomizationFile accepts either a kustomization file directly or a
// directory containing one, matching kustomize's own file discovery.
func resolveKustomizationFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read kustomization: %w", err)
	}
	if !info.IsDir() {
		return path, nil
	}
	for _, name := range konfig.RecognizedKustomizationFileNames() {
		candidate := filepath.Join(path, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no kustomization file found in %q", path)
}
