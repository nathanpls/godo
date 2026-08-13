package main

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

type ghCall struct {
	arguments []string
	input     string
}

func TestIssueTemplateRenderingAndManagedEdit(t *testing.T) {
	template, ok := findIssueTemplate("bug")
	if !ok {
		t.Fatal("bug template is missing")
	}
	body, err := renderIssueBody(template, map[string]string{
		"observed":  "cursor skipped an item",
		"expected":  "every item appears",
		"reproduce": "request two pages",
	})
	if err != nil {
		t.Fatal(err)
	}
	body += "\nMaintainer notes remain here.\n"
	updated, err := updateManagedIssue(body, map[string]string{"reproduce": "request three pages"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"template=bug version=1", "cursor skipped an item", "request three pages", "Maintainer notes remain here."} {
		if !strings.Contains(updated, want) {
			t.Fatalf("updated body does not contain %q:\n%s", want, updated)
		}
	}
	if strings.Contains(updated, "request two pages") {
		t.Fatalf("old field remained in body:\n%s", updated)
	}
	if _, err := updateManagedIssue("ordinary issue", map[string]string{"observed": "x"}); err == nil {
		t.Fatal("unmanaged issue accepted field edits")
	}
	if _, err := updateManagedIssue(body, map[string]string{"unknown": "x"}); err == nil {
		t.Fatal("unknown field was accepted")
	}
}

func TestIssueTemplateValidationAndDiscovery(t *testing.T) {
	var output strings.Builder
	application := &app{stdout: &output}
	if err := application.run([]string{"issue", "template", "feature", "--json"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"id":"feature"`, `"name":"problem"`, `"required":true`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("template JSON does not contain %q:\n%s", want, output.String())
		}
	}
	if _, err := parseIssueAdd([]string{"bug", "--title", "Broken", "--field", "observed=x"}); err == nil {
		t.Fatal("missing required fields were accepted")
	}
	if _, err := parseIssueAdd([]string{"bug", "--title", "Broken", "--field", "observed=x", "--field", "expected=y", "--field", "reproduce=z", "--field", "made-up=v"}); err == nil {
		t.Fatal("unknown field was accepted")
	}
}

func TestIssueAddUsesGHAndFixedRepository(t *testing.T) {
	var calls []ghCall
	var output strings.Builder
	application := &app{
		stdout: &output,
		runGH: func(input io.Reader, arguments ...string) ([]byte, error) {
			call := ghCall{arguments: slices.Clone(arguments)}
			if input != nil {
				body, err := io.ReadAll(input)
				if err != nil {
					t.Fatal(err)
				}
				call.input = string(body)
			}
			calls = append(calls, call)
			if arguments[0] == "auth" {
				return nil, nil
			}
			return []byte("https://github.com/nathanpls/godo/issues/1\n"), nil
		},
	}
	err := application.run([]string{
		"issue", "add", "bug", "--title", "Cursor skips an item",
		"--field", "observed=missing item", "--field", "expected=all items", "--field", "reproduce=request two pages",
		"--label", "help wanted", "--assignee", "@me",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || !slices.Equal(calls[0].arguments, []string{"auth", "status", "--hostname", "github.com"}) {
		t.Fatalf("gh calls = %+v", calls)
	}
	create := calls[1]
	for _, want := range []string{"issue", "create", "--body-file", "-", "--repo", godoIssueRepository, "--label", "bug", "--label", "help wanted", "--assignee", "@me"} {
		if !slices.Contains(create.arguments, want) {
			t.Fatalf("create arguments do not contain %q: %v", want, create.arguments)
		}
	}
	if strings.Contains(strings.Join(create.arguments, " "), "missing item") || !strings.Contains(create.input, "missing item") {
		t.Fatalf("body was not passed only through stdin: arguments=%v input=%q", create.arguments, create.input)
	}
	if !strings.Contains(output.String(), "Created issue #1: https://github.com/nathanpls/godo/issues/1") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestManagedEditRejectsDuplicateMarkers(t *testing.T) {
	template, _ := findIssueTemplate("task")
	body, err := renderIssueBody(template, map[string]string{"goal": "x", "steps": "y", "acceptance": "z"})
	if err != nil {
		t.Fatal(err)
	}
	for _, malformed := range []string{body + managedStart, body + "\n" + issueMarkerPattern.FindString(body)} {
		if _, err := updateManagedIssue(malformed, map[string]string{"goal": "new"}); err == nil {
			t.Fatal("ambiguous managed body was accepted")
		}
	}
}

func TestIssueAddDryRunDoesNotNeedGH(t *testing.T) {
	var output strings.Builder
	application := &app{
		stdout: &output,
		runGH: func(io.Reader, ...string) ([]byte, error) {
			t.Fatal("dry-run called gh")
			return nil, nil
		},
	}
	err := application.run([]string{
		"issue", "add", "task", "--title", "Ship issues", "--field", "goal=working command",
		"--field", "steps=implement and verify", "--field", "acceptance=commands pass", "--dry-run", "--json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"repository":"nathanpls/godo"`) || !strings.Contains(output.String(), `"template":"task"`) {
		t.Fatalf("dry-run output = %s", output.String())
	}
}

func TestIssueMetadataEditDryRunDoesNotNeedGH(t *testing.T) {
	var output strings.Builder
	application := &app{
		stdout: &output,
		runGH: func(io.Reader, ...string) ([]byte, error) {
			t.Fatal("metadata dry-run called gh")
			return nil, nil
		},
	}
	if err := application.run([]string{"issue", "edit", "12", "--title", "Updated", "--add-label", "bug", "--dry-run"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Title: Updated") || !strings.Contains(output.String(), "Add label: bug") {
		t.Fatalf("dry-run output = %q", output.String())
	}
}

func TestIssueContributorAndMaintainerCommands(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		operation string
		contains  []string
	}{
		{"list", []string{"issue", "list", "--state", "open", "--json"}, "list", []string{"--state", "open", "--json"}},
		{"search", []string{"issue", "search", "cursor pagination", "--label", "bug"}, "list", []string{"--search", "cursor pagination", "--label", "bug"}},
		{"get", []string{"issue", "get", "12", "--comments", "--json"}, "view", []string{"12", "--json", "comments"}},
		{"comment", []string{"issue", "comment", "12", "--body", "Confirmed"}, "comment", []string{"12", "--body-file", "-"}},
		{"close", []string{"issue", "close", "12", "--comment", "Fixed"}, "close", []string{"12", "--comment", "Fixed"}},
		{"reopen", []string{"issue", "reopen", "12"}, "reopen", []string{"12"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls []ghCall
			application := &app{
				stdout: &bytes.Buffer{},
				runGH: func(input io.Reader, arguments ...string) ([]byte, error) {
					call := ghCall{arguments: slices.Clone(arguments)}
					if input != nil {
						body, _ := io.ReadAll(input)
						call.input = string(body)
					}
					calls = append(calls, call)
					return []byte("ok\n"), nil
				},
			}
			if err := application.run(test.arguments); err != nil {
				t.Fatal(err)
			}
			if len(calls) != 2 || len(calls[1].arguments) < 2 || calls[1].arguments[0] != "issue" || calls[1].arguments[1] != test.operation {
				t.Fatalf("calls = %+v", calls)
			}
			remote := calls[1]
			if !containsSequence(remote.arguments, "--repo", godoIssueRepository) {
				t.Fatalf("repository is not fixed: %v", remote.arguments)
			}
			for _, want := range test.contains {
				if !argumentContains(remote.arguments, want) {
					t.Fatalf("arguments do not contain %q: %v", want, remote.arguments)
				}
			}
			if test.name == "comment" && remote.input != "Confirmed" {
				t.Fatalf("comment input = %q", remote.input)
			}
		})
	}
}

func TestIssueManagedFieldEdit(t *testing.T) {
	template, _ := findIssueTemplate("investigation")
	body, err := renderIssueBody(template, map[string]string{"question": "Which API?", "context": "Need issues", "deliverable": "Decision"})
	if err != nil {
		t.Fatal(err)
	}
	body += "\nMaintainer note.\n"
	var calls []ghCall
	application := &app{
		stdout: &bytes.Buffer{},
		runGH: func(input io.Reader, arguments ...string) ([]byte, error) {
			call := ghCall{arguments: slices.Clone(arguments)}
			if input != nil {
				value, _ := io.ReadAll(input)
				call.input = string(value)
			}
			calls = append(calls, call)
			if len(arguments) > 2 && arguments[0] == "issue" && arguments[1] == "view" {
				encoded, _ := writeJSONBytes(map[string]string{"body": body})
				return encoded, nil
			}
			return []byte("updated\n"), nil
		},
	}
	if err := application.run([]string{"issue", "edit", "4", "--field", "deliverable=Written recommendation", "--add-label", "help wanted"}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 {
		t.Fatalf("calls = %+v", calls)
	}
	edit := calls[2]
	if !strings.Contains(edit.input, "Written recommendation") || !strings.Contains(edit.input, "Maintainer note.") {
		t.Fatalf("edited body = %q", edit.input)
	}
	if !containsSequence(edit.arguments, "--add-label", "help wanted") || !containsSequence(edit.arguments, "--repo", godoIssueRepository) {
		t.Fatalf("edit arguments = %v", edit.arguments)
	}
}

func TestIssueAuthFailureExplainsSetup(t *testing.T) {
	application := &app{
		stdout: &bytes.Buffer{},
		runGH:  func(io.Reader, ...string) ([]byte, error) { return nil, errors.New("not logged in") },
	}
	err := application.run([]string{"issue", "list"})
	if err == nil || !strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("error = %v", err)
	}
}

func containsSequence(values []string, first, second string) bool {
	for index := 0; index+1 < len(values); index++ {
		if values[index] == first && values[index+1] == second {
			return true
		}
	}
	return false
}

func argumentContains(values []string, want string) bool {
	for _, value := range values {
		if value == want || strings.Contains(value, want) {
			return true
		}
	}
	return false
}

func writeJSONBytes(value any) ([]byte, error) {
	var output bytes.Buffer
	err := writeJSON(&output, value)
	return output.Bytes(), err
}
