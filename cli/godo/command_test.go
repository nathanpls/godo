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
	installed  []service
	configured []service
	restarted  []service
	removed    []service
}

func (s *fakeSupervisor) configure(service service, _ string) error {
	s.configured = append(s.configured, service)
	return nil
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
	options, err := parseAdd([]string{"--name=docs", "./docs", "--port", "8080", "--additions", "markdown", "--workdir", "./run", "--env-file=.env", "--no-agent", "--", "discord", "discord.json", "--verbose"})
	if err != nil {
		t.Fatal(err)
	}
	if options.target != "./docs" || options.name != "docs" || options.port != 8080 || options.additions != "markdown" || options.workDir != "./run" || options.envFile != ".env" || !options.noAgent || strings.Join(options.args, "|") != "discord|discord.json|--verbose" {
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

func TestParseServiceEditRuntimeSettings(t *testing.T) {
	options, err := parseServiceEdit([]string{"1", "--workdir", "./run", "--env-file=", "--no-agent", "--", "discord", "discord.json"})
	if err != nil {
		t.Fatal(err)
	}
	if !options.workDirSet || !options.envFileSet || !options.agentSet || !options.noAgent || !options.argsSet || strings.Join(options.args, "|") != "discord|discord.json" {
		t.Fatalf("options = %+v", options)
	}
	cleared, err := parseServiceEdit([]string{"1", "--"})
	if err != nil || !cleared.argsSet || len(cleared.args) != 0 {
		t.Fatalf("clear args = %+v, %v", cleared, err)
	}
}

func TestExecutableServiceLifecycle(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "godex")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(directory, "discord.env")
	if err := os.WriteFile(envFile, []byte("DISCORD_BOT_TOKEN=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	supervisor := &fakeSupervisor{}
	application := &app{
		store:      store{configDir: filepath.Join(directory, "config"), dataDir: filepath.Join(directory, "data")},
		supervisor: supervisor, agentsFile: filepath.Join(directory, "AGENTS.md"), stdout: &bytes.Buffer{}, cwd: directory,
	}
	if err := application.run([]string{"service", "add", executable, "--port", fmt.Sprint(port), "--env-file", envFile, "--no-agent", "--", "discord", "discord.json"}); err != nil {
		t.Fatal(err)
	}
	value, err := application.store.load()
	if err != nil {
		t.Fatal(err)
	}
	service := value.Services[0]
	if service.Kind != "executable" || service.Target != executable || service.WorkDir != directory || service.EnvFile != envFile || !service.NoAgent || strings.Join(service.Args, "|") != "discord|discord.json" {
		t.Fatalf("service = %+v", service)
	}
	managed, err := os.ReadFile(application.binaryPath(1))
	if err != nil || string(managed) != "#!/bin/sh\nexit 0\n" {
		t.Fatalf("managed executable = %q, %v", managed, err)
	}
	registryContent, err := os.ReadFile(filepath.Join(directory, "config", "services.json"))
	if err != nil || strings.Contains(string(registryContent), "secret") {
		t.Fatalf("registry contains environment secret: %q, %v", registryContent, err)
	}
	agents, err := os.ReadFile(application.agentsFile)
	if err != nil || strings.Contains(string(agents), service.Name) || !strings.Contains(string(agents), "No local services") {
		t.Fatalf("AGENTS.md = %q, %v", agents, err)
	}
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := application.run([]string{"service", "update", "1"}); err != nil {
		t.Fatal(err)
	}
	managed, err = os.ReadFile(application.binaryPath(1))
	if err != nil || string(managed) != "#!/bin/sh\nexit 1\n" {
		t.Fatalf("updated executable = %q, %v", managed, err)
	}
	if err := application.run([]string{"service", "restart", "1"}); err != nil {
		t.Fatal(err)
	}
	if len(supervisor.restarted) != 2 {
		t.Fatalf("restart calls = %d", len(supervisor.restarted))
	}
	if err := application.run([]string{"service", "edit", "1", "--agent", "--", "discord", "other.json"}); err != nil {
		t.Fatal(err)
	}
	if len(supervisor.configured) != 1 || supervisor.configured[0].NoAgent || strings.Join(supervisor.configured[0].Args, "|") != "discord|other.json" {
		t.Fatalf("configured = %+v", supervisor.configured)
	}
	output := application.stdout.(*bytes.Buffer)
	output.Reset()
	if err := application.run([]string{"service", "list"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"discord" "other.json"`) || !strings.Contains(output.String(), "AGENT") {
		t.Fatalf("service list = %q", output.String())
	}
}

func TestEnvironmentFileRequiresPrivatePermissions(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "service.env")
	if err := os.WriteFile(path, []byte("TOKEN=secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveEnvFile(directory, path); err == nil {
		t.Fatal("public environment file was accepted")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveEnvFile(directory, "service.env")
	if err != nil || resolved != path {
		t.Fatalf("resolved = %q, %v", resolved, err)
	}
	if err := os.WriteFile(path, []byte("PORT=9999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveEnvFile(directory, path); err == nil {
		t.Fatal("environment file defining PORT was accepted")
	}
}

func TestResolveExecutableTarget(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "tool")
	if err := os.WriteFile(target, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveTarget(directory, "tool")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.kind != "executable" || resolved.target != target || resolved.workDir != directory {
		t.Fatalf("resolved = %+v", resolved)
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
