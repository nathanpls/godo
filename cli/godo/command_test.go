package main

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeSupervisor struct {
	installed []service
	restarted []service
	removed   []service
}

func (s *fakeSupervisor) install(service service, _ string) error {
	s.installed = append(s.installed, service)
	return nil
}

func (s *fakeSupervisor) restart(service service) error {
	s.restarted = append(s.restarted, service)
	return nil
}

func (s *fakeSupervisor) remove(service service) error {
	s.removed = append(s.removed, service)
	return nil
}

func TestServiceLifecycle(t *testing.T) {
	directory := t.TempDir()
	project := filepath.Join(directory, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(project, "go.mod"), "module example\n\ngo 1.26.0\n")
	writeTestFile(t, filepath.Join(project, "main.go"), "package main\nfunc main() {}\n")

	port := freePort(t)
	var output bytes.Buffer
	supervisor := &fakeSupervisor{}
	application := &app{
		store: store{
			configDir: filepath.Join(directory, "config"),
			dataDir:   filepath.Join(directory, "data"),
		},
		supervisor: supervisor,
		agentsFile: filepath.Join(directory, "opencode", "AGENTS.md"),
		stdout:     &output,
		cwd:        directory,
	}

	if err := application.run([]string{"service", "add", project, "--name", "example", "--port", fmt.Sprint(port), "--additions", "local docs"}); err != nil {
		t.Fatal(err)
	}
	if len(supervisor.installed) != 1 || supervisor.installed[0].ID != 1 {
		t.Fatalf("installed services = %+v", supervisor.installed)
	}
	if _, err := os.Stat(application.binaryPath(1)); err != nil {
		t.Fatalf("built binary: %v", err)
	}

	agentContent, err := os.ReadFile(application.agentsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(agentContent), fmt.Sprintf("http://localhost:%d", port)) {
		t.Fatalf("AGENTS.md does not contain service URL:\n%s", agentContent)
	}

	if err := application.run([]string{"service", "update", "1"}); err != nil {
		t.Fatal(err)
	}
	if len(supervisor.restarted) != 1 {
		t.Fatalf("restart calls = %d, want 1", len(supervisor.restarted))
	}

	if err := application.run([]string{"service", "edit", "1", "--name", "updated", "--additions", "new instructions"}); err != nil {
		t.Fatal(err)
	}
	if len(supervisor.installed) != 1 || len(supervisor.restarted) != 1 || len(supervisor.removed) != 0 {
		t.Fatalf("edit called supervisor: installed=%d restarted=%d removed=%d", len(supervisor.installed), len(supervisor.restarted), len(supervisor.removed))
	}
	edited, err := application.store.load()
	if err != nil {
		t.Fatal(err)
	}
	if edited.Services[0].Name != "updated" || edited.Services[0].Additions != "new instructions" {
		t.Fatalf("edited service = %+v", edited.Services[0])
	}
	agentContent, err = os.ReadFile(application.agentsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(agentContent), "| 1 | updated |") || !strings.Contains(string(agentContent), "new instructions") {
		t.Fatalf("AGENTS.md was not updated:\n%s", agentContent)
	}

	if err := application.run([]string{"service", "remove", "1"}); err != nil {
		t.Fatal(err)
	}
	if len(supervisor.removed) != 1 {
		t.Fatalf("remove calls = %d, want 1", len(supervisor.removed))
	}
	value, err := application.store.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(value.Services) != 0 || value.NextID != 2 {
		t.Fatalf("registry after removal = %+v", value)
	}
}

func TestParseAddAcceptsOptionsAroundTarget(t *testing.T) {
	options, err := parseAdd([]string{"--name=docs", "./docs", "--port", "8080", "--additions", "markdown"})
	if err != nil {
		t.Fatal(err)
	}
	if options.target != "./docs" || options.name != "docs" || options.port != 8080 || options.additions != "markdown" {
		t.Fatalf("options = %+v", options)
	}
}

func TestParseServiceEditCanClearAdditions(t *testing.T) {
	options, err := parseServiceEdit([]string{"1", "--additions", ""})
	if err != nil {
		t.Fatal(err)
	}
	if options.id != 1 || !options.additionsSet || options.additions != "" || options.nameSet {
		t.Fatalf("options = %+v", options)
	}
	if _, err := parseServiceEdit([]string{"1"}); err == nil {
		t.Fatal("edit without changes was accepted")
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}
