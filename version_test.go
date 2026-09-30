package main

import "testing"

func TestVersionIsSemver(t *testing.T) {
	if version == "" {
		t.Fatal("version is empty; VERSION file not embedded")
	}
	for _, c := range version {
		if c == '\n' || c == ' ' {
			t.Fatalf("version %q contains whitespace; VERSION must be trimmed", version)
		}
	}
}
