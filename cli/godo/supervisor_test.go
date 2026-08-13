package main

import (
	"strings"
	"testing"
)

func TestUnitQuote(t *testing.T) {
	got := unitQuote(`/home/godo docs/100% "ready"`)
	want := `"/home/godo docs/100%% \"ready\""`
	if got != want {
		t.Fatalf("unitQuote() = %q, want %q", got, want)
	}
}

func TestUnitDescriptionRemovesNewlines(t *testing.T) {
	got := unitDescription("docs\nservice\rname")
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("unit description contains a newline: %q", got)
	}
}
