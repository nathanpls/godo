package main

import (
	"strings"
	"testing"
)

func TestRenderUnit(t *testing.T) {
	unit := renderUnit(service{ID: 1, Name: "local docs", WorkDir: "/home/godo docs", Args: []string{"discord", `config "one".json`, "100%"}, EnvFile: "/home/godo/.config/discord.env", Port: 41000}, "/home/.local/share/godo/service")
	if !strings.Contains(unit, "WorkingDirectory=/home/godo\\x20docs\n") {
		t.Fatalf("unit has an invalid working directory:\n%s", unit)
	}
	if !strings.Contains(unit, `ExecStart="/home/.local/share/godo/service" "discord" "config \"one\".json" "100%%"`+"\n") {
		t.Fatalf("unit has an invalid executable:\n%s", unit)
	}
	if !strings.Contains(unit, "EnvironmentFile=/home/godo/.config/discord.env\n") {
		t.Fatalf("unit has an invalid environment file:\n%s", unit)
	}
	if !strings.Contains(unit, "Environment=\"PORT=41000\"\n") {
		t.Fatalf("unit has an invalid port environment:\n%s", unit)
	}
}

func TestUnitQuote(t *testing.T) {
	got := unitQuote(`/home/$USER docs/100% "ready"`)
	want := `"/home/$$USER docs/100%% \"ready\""`
	if got != want {
		t.Fatalf("unitQuote() = %q, want %q", got, want)
	}
}

func TestUnitPath(t *testing.T) {
	got := unitPath(`/home/godo docs/100%`)
	want := `/home/godo\x20docs/100%%`
	if got != want {
		t.Fatalf("unitPath() = %q, want %q", got, want)
	}
}

func TestUnitDescriptionRemovesNewlines(t *testing.T) {
	got := unitDescription("docs\nservice\rname")
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("unit description contains a newline: %q", got)
	}
}
