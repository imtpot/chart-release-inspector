package main

import (
	"bytes"
	"os"
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
	for _, command := range []string{"inspect", "batch", "manifest validate", "version"} {
		if !bytes.Contains(output.Bytes(), []byte(command)) {
			t.Fatalf("usage output does not contain %q: %s", command, output.String())
		}
	}
}
