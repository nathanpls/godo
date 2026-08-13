package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSourceSearch(t *testing.T) {
	options, err := parseSourceSearch([]string{"needle", "--package=core/http/plugins/apikey", "--context", "5"})
	if err != nil {
		t.Fatal(err)
	}
	if options.query != "needle" || options.packagePath != "core/http/plugins/apikey" || options.context != 5 {
		t.Fatalf("options = %+v", options)
	}

	invalid := [][]string{
		nil,
		{"one", "two"},
		{"needle", "--package"},
		{"needle", "--package=../http"},
		{"needle", "--context=-1"},
		{"needle", "--context=nope"},
		{"needle", "--unknown"},
	}
	for _, arguments := range invalid {
		if _, err := parseSourceSearch(arguments); err == nil {
			t.Fatalf("invalid arguments were accepted: %q", arguments)
		}
	}
}

func TestSourceCommands(t *testing.T) {
	project := t.TempDir()
	writeTestFile(t, filepath.Join(project, "go.mod"), "module example.com/app\n")
	nested := filepath.Join(project, "internal", "app")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	source := t.TempDir()
	packageDir := filepath.Join(source, "core", "http", "plugins", "apikey")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(source, "outside.go"), "package godo\nneedle outside\n")
	writeTestFile(t, filepath.Join(packageDir, "plugin.go"), "package apikey\nbefore one\nneedle one\nafter one\ngap\nbefore two\nneedle two\nafter two\n")

	var output strings.Builder
	var resolvedPackage string
	application := &app{
		cwd:    nested,
		stdout: &output,
		resolveSource: func(root, packagePath string) (sourceLocation, error) {
			if root != project {
				t.Fatalf("root = %q, want %q", root, project)
			}
			resolvedPackage = packagePath
			return sourceLocation{moduleDir: source, targetDir: packageDir}, nil
		},
	}

	if err := application.run([]string{"source", "core/http/plugins/apikey"}); err != nil {
		t.Fatal(err)
	}
	if resolvedPackage != "core/http/plugins/apikey" || output.String() != packageDir+"\n" {
		t.Fatalf("resolved package = %q, output = %q", resolvedPackage, output.String())
	}

	output.Reset()
	if err := application.run([]string{"source", "search", "needle", "--package", "core/http/plugins/apikey", "--context=1"}); err != nil {
		t.Fatal(err)
	}
	want := "[core/http/plugins/apikey/plugin.go]\n" +
		"  2 before one\n" +
		"> 3 needle one\n" +
		"  4 after one\n" +
		"...\n" +
		"  6 before two\n" +
		"> 7 needle two\n" +
		"  8 after two\n"
	if output.String() != want {
		t.Fatalf("search output:\n%s\nwant:\n%s", output.String(), want)
	}
}

func TestSearchSourceIsDeterministic(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "z.go"), "package source\r\nneedle z\r\n")
	writeTestFile(t, filepath.Join(source, "a_test.go"), "//go:build custom\npackage source\nneedle a\n")
	if err := os.Mkdir(filepath.Join(source, ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(source, ".hidden", "ignored.go"), "needle hidden\n")

	var output strings.Builder
	if err := searchSource(&output, source, source, "needle", 0); err != nil {
		t.Fatal(err)
	}
	want := "a_test.go:3:needle a\nz.go:2:needle z\n"
	if output.String() != want {
		t.Fatalf("search output = %q, want %q", output.String(), want)
	}

	output.Reset()
	if err := searchSource(&output, source, source, "needle z", 1); err != nil {
		t.Fatal(err)
	}
	want = "[z.go]\n  1 package source\n> 2 needle z\n"
	if output.String() != want {
		t.Fatalf("end-of-file context = %q, want %q", output.String(), want)
	}

	output.Reset()
	if err := searchSource(&output, source, source, "absent", 0); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("no-match output = %q", output.String())
	}
}

func TestResolveGodoSourceUsesProjectReplacement(t *testing.T) {
	project := t.TempDir()
	repository, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(project, "go.mod"), "module example.com/app\n\ngo 1.26.0\n\nrequire github.com/nathanpls/godo v0.0.0\n\nreplace github.com/nathanpls/godo => "+repository+"\n")

	location, err := resolveGodoSource(project, "core/http/plugins/apikey")
	if err != nil {
		t.Fatal(err)
	}
	if location.moduleDir != repository || location.targetDir != filepath.Join(repository, "core", "http", "plugins", "apikey") {
		t.Fatalf("location = %+v", location)
	}
}

func TestValidateSourcePackage(t *testing.T) {
	valid := []string{".", "core/http", "core/http/plugins/apikey", "channels/discord"}
	for _, packagePath := range valid {
		if err := validateSourcePackage(packagePath); err != nil {
			t.Fatalf("valid package %q: %v", packagePath, err)
		}
	}
	invalid := []string{"", "/http", "../http", "http/../orm", `http\plugins`}
	for _, packagePath := range invalid {
		if err := validateSourcePackage(packagePath); err == nil {
			t.Fatalf("invalid package was accepted: %q", packagePath)
		}
	}
}
