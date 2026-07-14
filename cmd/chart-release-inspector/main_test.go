package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestValidOutputRejectsRichAlias(t *testing.T) {
	if !validOutput("terminal") || !validOutput("json") {
		t.Fatal("validOutput() rejected a supported output format")
	}
	if validOutput("rich") {
		t.Fatal("validOutput() accepted the removed rich alias")
	}
}

func TestUseColorAcceptsExplicitModes(t *testing.T) {
	always, err := useColor("always")
	if err != nil || !always {
		t.Fatalf("useColor(always) = %t, %v", always, err)
	}
	never, err := useColor("never")
	if err != nil || never {
		t.Fatalf("useColor(never) = %t, %v", never, err)
	}
}

func TestUseColorRejectsUnknownMode(t *testing.T) {
	if _, err := useColor("vivid"); err == nil {
		t.Fatal("useColor() accepted an unknown color mode")
	}
}

func TestPrintUsageListsCommands(t *testing.T) {
	temporaryFile, err := os.CreateTemp(t.TempDir(), "usage")
	if err != nil {
		t.Fatal(err)
	}
	defer temporaryFile.Close()

	printUsage(temporaryFile)
	if _, err := temporaryFile.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if _, err := output.ReadFrom(temporaryFile); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"inspect", "batch", "config validate", "version"} {
		if !bytes.Contains(output.Bytes(), []byte(command)) {
			t.Fatalf("usage output does not contain %q: %s", command, output.String())
		}
	}
}

func TestValidateConfigFile(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "release-notes.yaml")
	contents := "rules:\n  - chart: example\n    provider: github\n    repository: https://github.com/example/project\n"
	if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	ruleCount, err := validateConfigFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if ruleCount != 1 {
		t.Fatalf("validateConfigFile() rule count = %d, want 1", ruleCount)
	}
}

func TestValidateConfigFileRejectsInvalidProvider(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "release-notes.yaml")
	contents := "rules:\n  - chart: example\n    provider: unsupported\n"
	if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateConfigFile(filename); err == nil {
		t.Fatal("validateConfigFile() accepted an unsupported provider")
	}
}
