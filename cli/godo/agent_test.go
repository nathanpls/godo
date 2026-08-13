package main

import (
	"strings"
	"testing"
)

func TestUpdateAgentContent(t *testing.T) {
	services := []service{{ID: 1, Name: "docs", Port: 41000, Additions: "Use `Accept: text/markdown`"}}
	block := agentBlock(services)
	original := "# Instructions\n\nKeep this text.\n"

	added, err := updateAgentContent(original, block)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(added, original) {
		t.Fatalf("content outside the managed block changed:\n%s", added)
	}
	if !strings.Contains(added, "| 1 | docs | http://localhost:41000 | Use `Accept: text/markdown` |") {
		t.Fatalf("service was not added to agent content:\n%s", added)
	}

	updatedBlock := agentBlock([]service{{ID: 2, Name: "api", Port: 41001}})
	updated, err := updateAgentContent(added, updatedBlock)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(updated, "localhost:41000") || !strings.Contains(updated, "localhost:41001") {
		t.Fatalf("managed block was not replaced:\n%s", updated)
	}
	if strings.Count(updated, agentBlockStart) != 1 || strings.Count(updated, agentBlockEnd) != 1 {
		t.Fatalf("managed block was duplicated:\n%s", updated)
	}
}

func TestUpdateAgentContentRejectsMalformedBlock(t *testing.T) {
	if _, err := updateAgentContent("before\n<godo>\n", agentBlock(nil)); err == nil {
		t.Fatal("malformed block was accepted")
	}
	if _, err := updateAgentContent("<godo>\n<godo>\n</godo>", agentBlock(nil)); err == nil {
		t.Fatal("duplicated block was accepted")
	}
}

func TestAgentBlockEscapesTableCells(t *testing.T) {
	block := agentBlock([]service{{ID: 1, Name: "docs|local", Port: 41000, Additions: "line one\nline two"}})
	if !strings.Contains(block, "docs\\|local") || !strings.Contains(block, "line one line two") {
		t.Fatalf("table values were not escaped:\n%s", block)
	}
}
