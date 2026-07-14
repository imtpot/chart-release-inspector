package main

import "testing"

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
