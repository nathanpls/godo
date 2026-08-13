package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanpls/godo/http/plugins/apikey"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestAuthLifecycle(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n")
	child := filepath.Join(root, "internal")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	application := &app{cwd: child, stdout: &output}

	if err := application.run([]string{"auth", "init"}); err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(root, ".godo", "auth.json")
	info, err := os.Stat(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("auth mode = %o", info.Mode().Perm())
	}
	assertFileContains(t, filepath.Join(root, ".godo", ".gitignore"), "*\n!.gitignore\n")

	output.Reset()
	if err := application.run([]string{"auth", "create", "--name", "opencode"}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	token := lines[len(lines)-1]
	if !strings.HasPrefix(token, "godo_") {
		t.Fatalf("output does not end in token: %q", output.String())
	}
	if content, err := os.ReadFile(authPath); err != nil || strings.Contains(string(content), token) {
		t.Fatalf("plaintext token stored; error = %v", err)
	}

	output.Reset()
	if err := application.run([]string{"auth", "list"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "opencode") || !strings.Contains(output.String(), "godo_") || strings.Contains(output.String(), token) {
		t.Fatalf("list output = %q", output.String())
	}

	output.Reset()
	if err := application.run([]string{"auth", "revoke", "1"}); err != nil {
		t.Fatal(err)
	}
	if _, valid, err := apikey.NewFileStore(authPath).Authenticate(token); err != nil || valid {
		t.Fatalf("revoked token valid = %t, error = %v", valid, err)
	}
}

func TestAuthRequiresInitialization(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n")
	application := &app{cwd: root, stdout: &strings.Builder{}}
	if err := application.run([]string{"auth", "create", "--name", "agent"}); err == nil {
		t.Fatal("key was created without auth initialization")
	}
	if err := application.run([]string{"auth", "revoke", "1"}); err == nil {
		t.Fatal("key was revoked without auth initialization")
	}
}

func TestAuthInitRejectsUnsafeGitignore(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n")
	if err := os.Mkdir(filepath.Join(root, ".godo"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, ".godo", ".gitignore"), "# does not ignore secrets\n")
	application := &app{cwd: root, stdout: &strings.Builder{}}
	if err := application.run([]string{"auth", "init"}); err == nil {
		t.Fatal("unsafe existing .gitignore was accepted")
	}
	if _, err := os.Stat(filepath.Join(root, ".godo", "auth.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("auth file was created despite unsafe .gitignore: %v", err)
	}
}

func TestAuthCreateRevokesAfterOutputFailure(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n")
	setup := &app{cwd: root, stdout: &strings.Builder{}}
	if err := setup.run([]string{"auth", "init"}); err != nil {
		t.Fatal(err)
	}
	application := &app{cwd: root, stdout: failingWriter{}}
	if err := application.run([]string{"auth", "create", "--name", "lost"}); err == nil {
		t.Fatal("output failure was ignored")
	}
	keys, err := apikey.ListKeys(filepath.Join(root, ".godo", "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Fatalf("inaccessible key remains active: %+v", keys)
	}
}

func TestParseAuthCreate(t *testing.T) {
	name, scopes, err := parseAuthCreate([]string{"--name=agent", "--scope", "plans:write", "--scope=plans:read"})
	if err != nil || name != "agent" || len(scopes) != 2 {
		t.Fatalf("name = %q, scopes = %v, error = %v", name, scopes, err)
	}
	if _, _, err := parseAuthCreate(nil); err == nil {
		t.Fatal("missing name was accepted")
	}
}
