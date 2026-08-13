package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitProject(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder
	application := &app{cwd: root, stdout: &output}
	options, err := parseProjectInit([]string{"api", "--module", "example.com/api"})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.initProject(options); err != nil {
		t.Fatal(err)
	}

	project := filepath.Join(root, "api")
	assertFileContains(t, filepath.Join(project, "go.mod"), "module example.com/api\n")
	assertFileContains(t, filepath.Join(project, "main.go"), "package main\n")
	assertFileContains(t, filepath.Join(project, ".gitignore"), ".env\n")
	if !strings.Contains(output.String(), "godo add http") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestInitProjectInfersModule(t *testing.T) {
	root := t.TempDir()
	application := &app{cwd: root, stdout: &strings.Builder{}}
	if err := application.initProject(projectInitOptions{directory: "service"}); err != nil {
		t.Fatal(err)
	}
	assertFileContains(t, filepath.Join(root, "service", "go.mod"), "module service\n")
}

func TestInitProjectRejectsNonEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "existing.txt"), "keep")
	application := &app{cwd: root, stdout: &strings.Builder{}}
	if err := application.initProject(projectInitOptions{directory: ".", module: "example.com/app"}); err == nil {
		t.Fatal("non-empty project directory was accepted")
	}
	assertFileContains(t, filepath.Join(root, "existing.txt"), "keep")
}

func TestInitProjectValidatesBeforeCreatingDirectory(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "app")
	application := &app{cwd: root, stdout: &strings.Builder{}}
	if err := application.initProject(projectInitOptions{directory: "app", module: "bad path"}); err == nil {
		t.Fatal("invalid module path was accepted")
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid project left target directory: %v", err)
	}
}

func TestParseProjectInit(t *testing.T) {
	options, err := parseProjectInit([]string{".", "--module=example.com/app"})
	if err != nil {
		t.Fatal(err)
	}
	if options.directory != "." || options.module != "example.com/app" {
		t.Fatalf("options = %+v", options)
	}
	if _, err := parseProjectInit([]string{".", "other"}); err == nil {
		t.Fatal("multiple directories were accepted")
	}
	if _, err := parseProjectInit([]string{"app", "--module", "bad path"}); err != nil {
		t.Fatalf("module path is validated during initialization, not parsing: %v", err)
	}
}

func TestAddDependency(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n")
	child := filepath.Join(root, "internal")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	var gotRoot, gotDependency string
	var output strings.Builder
	application := &app{
		cwd: child, stdout: &output,
		goGet: func(root, dependency string) error {
			gotRoot, gotDependency = root, dependency
			return nil
		},
	}
	if err := application.addDependency([]string{"ratelimit"}); err != nil {
		t.Fatal(err)
	}
	if gotRoot != root || gotDependency != "github.com/nathanpls/godo/http/plugins/ratelimit" {
		t.Fatalf("go get = %q in %q", gotDependency, gotRoot)
	}
	if !strings.Contains(output.String(), "http://localhost:41000/http/plugins/ratelimit") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestAddDependencyError(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n")
	want := errors.New("offline")
	application := &app{
		cwd: root, stdout: &strings.Builder{},
		goGet: func(string, string) error { return want },
	}
	if err := application.addDependency([]string{"unknown"}); err == nil {
		t.Fatal("unknown dependency was accepted")
	}
	if err := application.addDependency([]string{"http"}); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestAgentHTTPDependenciesAreRegistered(t *testing.T) {
	for name, path := range map[string]string{
		"agentapi":    "github.com/nathanpls/godo/http/plugins/agentapi",
		"idempotency": "github.com/nathanpls/godo/http/plugins/idempotency",
		"requestid":   "github.com/nathanpls/godo/http/plugins/requestid",
	} {
		if dependencies[name].path != path {
			t.Fatalf("dependency %q = %q", name, dependencies[name].path)
		}
	}
}

func assertFileContains(t *testing.T, path, text string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), text) {
		t.Fatalf("%s does not contain %q:\n%s", path, text, content)
	}
}
