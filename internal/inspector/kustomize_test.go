package inspector

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadKustomizationManifestConvertsHelmCharts(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "kustomization.yaml")
	contents := `helmCharts:
  - name: kserve-crd
    version: v0.19.0
    repo: oci://ghcr.io/kserve/charts
  - name: external-secrets
    version: 2.6.0
    repo: https://charts.external-secrets.io
`
	if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	manifest, err := LoadKustomizationManifest(filename)
	if err != nil {
		t.Fatalf("LoadKustomizationManifest() error: %v", err)
	}
	if len(manifest.Charts) != 2 {
		t.Fatalf("len(manifest.Charts) = %d, want 2", len(manifest.Charts))
	}

	oci := manifest.Charts[0]
	if oci.Chart != "oci://ghcr.io/kserve/charts/kserve-crd" || oci.Repository != "" || oci.Version != "v0.19.0" {
		t.Fatalf("oci chart = %+v", oci)
	}

	classic := manifest.Charts[1]
	if classic.Chart != "external-secrets" || classic.Repository != "https://charts.external-secrets.io" || classic.Version != "2.6.0" {
		t.Fatalf("classic chart = %+v", classic)
	}
}

func TestLoadKustomizationManifestResolvesDirectory(t *testing.T) {
	dir := t.TempDir()
	contents := "helmCharts:\n  - name: example\n    version: 1.0.0\n    repo: https://example.test/charts\n"
	if err := os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	manifest, err := LoadKustomizationManifest(dir)
	if err != nil {
		t.Fatalf("LoadKustomizationManifest() error: %v", err)
	}
	if len(manifest.Charts) != 1 || manifest.Charts[0].Chart != "example" {
		t.Fatalf("manifest.Charts = %+v", manifest.Charts)
	}
}

func TestLoadKustomizationManifestRejectsEmptyHelmCharts(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "kustomization.yaml")
	if err := os.WriteFile(filename, []byte("resources:\n  - deployment.yaml\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadKustomizationManifest(filename); err == nil {
		t.Fatal("LoadKustomizationManifest() error = nil, want error for missing helmCharts")
	}
}
