package main

import (
	"strings"
	"testing"
)

func TestCommandHelp(t *testing.T) {
	tests := []struct {
		arguments []string
		contains  []string
	}{
		{[]string{"--help"}, []string{"godo makes", "init", "service", "db", "source", "issue"}},
		{[]string{"init", "--help"}, []string{"godo init [directory]", "--module", "--template", "discord"}},
		{[]string{"add", "--help"}, []string{"godo add <package>", "discord", "ratelimit", "postgres"}},
		{[]string{"auth", "--help"}, []string{"Manage bearer API keys", "create", "revoke"}},
		{[]string{"auth", "create", "--help"}, []string{"--name", "displayed once"}},
		{[]string{"api", "--help"}, []string{"agent discovery", "check"}},
		{[]string{"api", "check", "--help"}, []string{"godo api check", "OpenAPI", "request ID"}},
		{[]string{"issue", "--help"}, []string{"nathanpls/godo", "templates", "comment", "reopen", "gh CLI"}},
		{[]string{"issue", "add", "--help"}, []string{"godo issue add", "--field", "--dry-run"}},
		{[]string{"issue", "edit", "--help"}, []string{"godo issue edit", "--add-label", "managed body"}},
		{[]string{"service", "--help"}, []string{"Manage persistent", "update", "remove"}},
		{[]string{"service", "add", "--help"}, []string{"godo service add", "--additions"}},
		{[]string{"service", "edit", "--help"}, []string{"godo service edit", "without rebuilding", "use \"\" to clear"}},
		{[]string{"db", "--help"}, []string{"Generate and run", "generate", "rollback"}},
		{[]string{"db", "init", "--help"}, []string{"--dialect", "sqlite", "postgres"}},
		{[]string{"db", "generate", "--help"}, []string{"schema.lock.json", "--empty"}},
		{[]string{"db", "migrate", "--help"}, []string{"Apply all pending"}},
		{[]string{"source", "--help"}, []string{"godo source <package>", "selected module version", "search"}},
		{[]string{"source", "search", "--help"}, []string{"godo source search <query>", "--package", "--context", "groups lines"}},
		{[]string{"agent", "--help"}, []string{"global OpenCode AGENTS.md"}},
	}

	for _, test := range tests {
		t.Run(strings.Join(test.arguments, "_"), func(t *testing.T) {
			var output strings.Builder
			application := &app{stdout: &output}
			if err := application.run(test.arguments); err != nil {
				t.Fatal(err)
			}
			for _, want := range test.contains {
				if !strings.Contains(output.String(), want) {
					t.Fatalf("help does not contain %q:\n%s", want, output.String())
				}
			}
		})
	}
}
