package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

const (
	agentBlockStart = "<godo>"
	agentBlockEnd   = "</godo>"
)

func syncAgents(path string, services []service) error {
	content, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read AGENTS.md: %w", err)
	}
	updated, err := updateAgentContent(string(content), agentBlock(services))
	if err != nil {
		return err
	}
	if string(content) == updated {
		return nil
	}
	if err := writeAtomic(path, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write AGENTS.md: %w", err)
	}
	return nil
}

func agentBlock(services []service) string {
	services = slices.Clone(services)
	slices.SortFunc(services, func(a, b service) int { return a.ID - b.ID })

	var result strings.Builder
	result.WriteString(agentBlockStart)
	result.WriteString("\n## Local godo services\n\n")
	if len(services) == 0 {
		result.WriteString("No local services are currently managed by godo.\n")
	} else {
		result.WriteString("Use these locally running services when relevant:\n\n")
		result.WriteString("| ID | Name | URL | Additions |\n")
		result.WriteString("|---:|---|---|---|\n")
		for _, service := range services {
			fmt.Fprintf(&result, "| %d | %s | http://localhost:%d | %s |\n", service.ID, markdownCell(service.Name), service.Port, markdownCell(service.Additions))
		}
	}
	result.WriteString(agentBlockEnd)
	return result.String()
}

func markdownCell(value string) string {
	value = strings.NewReplacer("\r", " ", "\n", " ", "|", "\\|").Replace(value)
	return strings.TrimSpace(value)
}

func updateAgentContent(content, block string) (string, error) {
	if strings.Count(content, agentBlockStart) > 1 || strings.Count(content, agentBlockEnd) > 1 {
		return "", errors.New("AGENTS.md contains multiple <godo> blocks")
	}
	start := strings.Index(content, agentBlockStart)
	end := strings.Index(content, agentBlockEnd)
	if (start == -1) != (end == -1) || (start >= 0 && end < start) {
		return "", errors.New("AGENTS.md contains a malformed <godo> block")
	}
	if start >= 0 {
		after := end + len(agentBlockEnd)
		return content[:start] + block + content[after:], nil
	}

	if content == "" {
		return block + "\n", nil
	}
	return strings.TrimRight(content, "\n") + "\n\n" + block + "\n", nil
}
