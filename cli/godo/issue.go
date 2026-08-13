package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	godoIssueRepository = "nathanpls/godo"
	managedStart        = "<!-- godo:managed:start -->"
	managedEnd          = "<!-- godo:managed:end -->"
	maxIssueBodyBytes   = 65_000
)

var issueMarkerPattern = regexp.MustCompile(`<!-- godo:issue template=([a-z-]+) version=([0-9]+) -->`)

type ghRunner func(io.Reader, ...string) ([]byte, error)

type issueField struct {
	Name        string `json:"name"`
	Heading     string `json:"heading"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

type issueTemplate struct {
	ID            string       `json:"id"`
	Summary       string       `json:"summary"`
	DefaultLabels []string     `json:"default_labels,omitempty"`
	Fields        []issueField `json:"fields"`
}

var issueTemplates = []issueTemplate{
	{
		ID: "bug", Summary: "Report behavior that is incorrect or broken", DefaultLabels: []string{"bug"},
		Fields: []issueField{
			{Name: "observed", Heading: "Observed behavior", Description: "What actually happened", Required: true},
			{Name: "expected", Heading: "Expected behavior", Description: "What should have happened", Required: true},
			{Name: "reproduce", Heading: "Reproduction", Description: "Minimal repeatable steps", Required: true},
			{Name: "area", Heading: "Area", Description: "Affected package or command"},
			{Name: "environment", Heading: "Environment", Description: "Relevant versions, OS, and runtime"},
			{Name: "evidence", Heading: "Evidence", Description: "Logs or other relevant output"},
		},
	},
	{
		ID: "feature", Summary: "Propose a user capability", DefaultLabels: []string{"enhancement"},
		Fields: []issueField{
			{Name: "problem", Heading: "Problem", Description: "The user pain, independent of a solution", Required: true},
			{Name: "outcome", Heading: "Desired outcome", Description: "Observable success", Required: true},
			{Name: "proposal", Heading: "Proposal", Description: "The smallest useful behavior", Required: true},
			{Name: "use-case", Heading: "Use case", Description: "A concrete workflow"},
			{Name: "non-goals", Heading: "Non-goals", Description: "Explicit boundaries"},
			{Name: "alternatives", Heading: "Alternatives", Description: "Current workarounds or other approaches"},
		},
	},
	{
		ID: "task", Summary: "Record known implementation or maintenance work",
		Fields: []issueField{
			{Name: "goal", Heading: "Goal", Description: "What must become true", Required: true},
			{Name: "steps", Heading: "Steps", Description: "Ordered implementation work", Required: true},
			{Name: "acceptance", Heading: "Acceptance", Description: "Checks that prove completion", Required: true},
			{Name: "context", Heading: "Context", Description: "Why the work exists"},
			{Name: "dependencies", Heading: "Dependencies", Description: "Blocking issues or systems"},
			{Name: "risks", Heading: "Risks", Description: "Known failure modes"},
		},
	},
	{
		ID: "investigation", Summary: "Research an open question before implementation", DefaultLabels: []string{"question"},
		Fields: []issueField{
			{Name: "question", Heading: "Question", Description: "The decision or answer needed", Required: true},
			{Name: "context", Heading: "Context", Description: "What prompted the investigation", Required: true},
			{Name: "deliverable", Heading: "Deliverable", Description: "The expected artifact or answer", Required: true},
			{Name: "hypotheses", Heading: "Hypotheses", Description: "Ideas to verify"},
			{Name: "constraints", Heading: "Constraints", Description: "Time or system limits"},
			{Name: "references", Heading: "References", Description: "Relevant code, documents, or issues"},
		},
	},
}

type issueAddOptions struct {
	template  issueTemplate
	title     string
	fields    map[string]string
	labels    []string
	assignees []string
	dryRun    bool
	json      bool
}

type issueListOptions struct {
	query    string
	state    string
	labels   []string
	author   string
	assignee string
	limit    int
	json     bool
}

type issueEditOptions struct {
	number          int
	title           string
	titleSet        bool
	fields          map[string]string
	addLabels       []string
	removeLabels    []string
	addAssignees    []string
	removeAssignees []string
	dryRun          bool
}

func (a *app) runIssue(arguments []string) error {
	if len(arguments) == 0 || isHelp(arguments) {
		return printHelp(a.stdout, issueHelp)
	}
	switch arguments[0] {
	case "templates":
		if isHelp(arguments[1:]) {
			return printHelp(a.stdout, "Usage:\n  godo issue templates [--json]")
		}
		return a.listIssueTemplates(arguments[1:])
	case "template":
		if isHelp(arguments[1:]) {
			return printHelp(a.stdout, "Usage:\n  godo issue template <name> [--json]")
		}
		return a.describeIssueTemplate(arguments[1:])
	case "add":
		if isHelp(arguments[1:]) {
			return printHelp(a.stdout, issueAddHelp)
		}
		options, err := parseIssueAdd(arguments[1:])
		if err != nil {
			return err
		}
		return a.addIssue(options)
	case "list", "search":
		if isHelp(arguments[1:]) {
			return printHelp(a.stdout, issueListHelp)
		}
		options, err := parseIssueList(arguments[1:], arguments[0] == "search")
		if err != nil {
			return err
		}
		return a.listIssues(options)
	case "get":
		if isHelp(arguments[1:]) {
			return printHelp(a.stdout, issueGetHelp)
		}
		return a.getIssue(arguments[1:])
	case "edit":
		if isHelp(arguments[1:]) {
			return printHelp(a.stdout, issueEditHelp)
		}
		options, err := parseIssueEdit(arguments[1:])
		if err != nil {
			return err
		}
		return a.editIssue(options)
	case "comment":
		if isHelp(arguments[1:]) {
			return printHelp(a.stdout, issueCommentHelp)
		}
		return a.commentIssue(arguments[1:])
	case "close", "reopen":
		if isHelp(arguments[1:]) {
			help := issueCloseHelp
			if arguments[0] == "reopen" {
				help = issueReopenHelp
			}
			return printHelp(a.stdout, help)
		}
		return a.changeIssueState(arguments[0], arguments[1:])
	default:
		return fmt.Errorf("unknown issue command %q; run godo issue --help", arguments[0])
	}
}

func (a *app) listIssueTemplates(arguments []string) error {
	jsonOutput, err := parseOnlyJSON(arguments)
	if err != nil {
		return err
	}
	if jsonOutput {
		return writeJSON(a.stdout, issueTemplates)
	}
	for _, template := range issueTemplates {
		fmt.Fprintf(a.stdout, "%s\t%s\n", template.ID, template.Summary)
	}
	return nil
}

func (a *app) describeIssueTemplate(arguments []string) error {
	if len(arguments) < 1 || len(arguments) > 2 {
		return errors.New("issue template requires one template name and optional --json")
	}
	template, ok := findIssueTemplate(arguments[0])
	if !ok {
		return unknownIssueTemplate(arguments[0])
	}
	jsonOutput, err := parseOnlyJSON(arguments[1:])
	if err != nil {
		return err
	}
	if jsonOutput {
		return writeJSON(a.stdout, template)
	}
	fmt.Fprintf(a.stdout, "%s: %s\n", template.ID, template.Summary)
	if len(template.DefaultLabels) != 0 {
		fmt.Fprintf(a.stdout, "Default labels: %s\n", strings.Join(template.DefaultLabels, ", "))
	}
	fmt.Fprintln(a.stdout, "Fields:")
	for _, field := range template.Fields {
		requirement := "optional"
		if field.Required {
			requirement = "required"
		}
		fmt.Fprintf(a.stdout, "  %s (%s): %s\n", field.Name, requirement, field.Description)
	}
	return nil
}

func parseIssueAdd(arguments []string) (issueAddOptions, error) {
	var options issueAddOptions
	if len(arguments) == 0 || strings.HasPrefix(arguments[0], "-") {
		return options, errors.New("issue add requires a template name")
	}
	template, ok := findIssueTemplate(arguments[0])
	if !ok {
		return options, unknownIssueTemplate(arguments[0])
	}
	options.template = template
	options.fields = make(map[string]string)
	for index := 1; index < len(arguments); index++ {
		name, value, consumed, err := issueOption(arguments, index)
		if err != nil {
			return options, err
		}
		index += consumed
		switch name {
		case "--title":
			options.title = value
		case "--field":
			if err := addIssueField(options.fields, value); err != nil {
				return options, err
			}
		case "--label":
			options.labels = append(options.labels, value)
		case "--assignee":
			options.assignees = append(options.assignees, value)
		case "--dry-run":
			options.dryRun = true
		case "--json":
			options.json = true
		default:
			return options, fmt.Errorf("unknown issue add option %q", name)
		}
	}
	if err := validIssueMetadata("title", options.title); err != nil {
		return options, err
	}
	if err := validateTemplateFields(template, options.fields, true); err != nil {
		return options, err
	}
	if err := validateIssueValues("label", options.labels); err != nil {
		return options, err
	}
	if err := validateIssueValues("assignee", options.assignees); err != nil {
		return options, err
	}
	return options, nil
}

func (a *app) addIssue(options issueAddOptions) error {
	body, err := renderIssueBody(options.template, options.fields)
	if err != nil {
		return err
	}
	labels := slices.Concat(options.template.DefaultLabels, options.labels)
	labels = compactStrings(labels)
	if options.dryRun {
		if options.json {
			return writeJSON(a.stdout, struct {
				Repository string   `json:"repository"`
				Template   string   `json:"template"`
				Title      string   `json:"title"`
				Body       string   `json:"body"`
				Labels     []string `json:"labels"`
			}{godoIssueRepository, options.template.ID, options.title, body, labels})
		}
		fmt.Fprintf(a.stdout, "Repository: %s\nTemplate: %s\nTitle: %s\nLabels: %s\n\n%s", godoIssueRepository, options.template.ID, options.title, strings.Join(labels, ", "), body)
		return nil
	}
	arguments := []string{"create", "--title", options.title, "--body-file", "-"}
	for _, label := range labels {
		arguments = append(arguments, "--label", label)
	}
	for _, assignee := range options.assignees {
		arguments = append(arguments, "--assignee", assignee)
	}
	output, err := a.runIssueGH(strings.NewReader(body), arguments...)
	if err != nil {
		return err
	}
	url := strings.TrimSpace(string(output))
	number, err := issueNumberFromURL(url)
	if err != nil {
		return err
	}
	if options.json {
		return writeJSON(a.stdout, struct {
			Repository string `json:"repository"`
			Number     int    `json:"number"`
			URL        string `json:"url"`
		}{godoIssueRepository, number, url})
	}
	_, err = fmt.Fprintf(a.stdout, "Created issue #%d: %s\n", number, url)
	return err
}

func parseIssueList(arguments []string, allowQuery bool) (issueListOptions, error) {
	options := issueListOptions{state: "open", limit: 30}
	for index := 0; index < len(arguments); index++ {
		if !strings.HasPrefix(arguments[index], "-") {
			if !allowQuery || options.query != "" {
				return options, fmt.Errorf("unexpected issue search argument %q", arguments[index])
			}
			options.query = arguments[index]
			continue
		}
		name, value, consumed, err := issueOption(arguments, index)
		if err != nil {
			return options, err
		}
		index += consumed
		switch name {
		case "--state":
			if value != "open" && value != "closed" && value != "all" {
				return options, fmt.Errorf("invalid issue state %q", value)
			}
			options.state = value
		case "--label":
			options.labels = append(options.labels, value)
		case "--author":
			options.author = value
		case "--assignee":
			options.assignee = value
		case "--limit":
			limit, err := strconv.Atoi(value)
			if err != nil || limit < 1 || limit > 1000 {
				return options, fmt.Errorf("invalid issue limit %q", value)
			}
			options.limit = limit
		case "--json":
			options.json = true
		default:
			return options, fmt.Errorf("unknown issue search option %q", name)
		}
	}
	if err := validateIssueValues("label", options.labels); err != nil {
		return options, err
	}
	if options.author != "" {
		if err := validIssueMetadata("author", options.author); err != nil {
			return options, err
		}
	}
	if options.assignee != "" {
		if err := validIssueMetadata("assignee", options.assignee); err != nil {
			return options, err
		}
	}
	return options, nil
}

func (a *app) listIssues(options issueListOptions) error {
	arguments := []string{"list", "--state", options.state, "--limit", strconv.Itoa(options.limit)}
	if options.query != "" {
		arguments = append(arguments, "--search", options.query)
	}
	for _, label := range options.labels {
		arguments = append(arguments, "--label", label)
	}
	if options.author != "" {
		arguments = append(arguments, "--author", options.author)
	}
	if options.assignee != "" {
		arguments = append(arguments, "--assignee", options.assignee)
	}
	if options.json {
		arguments = append(arguments, "--json", "number,title,state,labels,url,author,createdAt,updatedAt")
	}
	return a.writeIssueGH(nil, arguments...)
}

func (a *app) getIssue(arguments []string) error {
	if len(arguments) < 1 {
		return errors.New("issue get requires one issue number")
	}
	number, err := parseIssueNumber(arguments[0])
	if err != nil {
		return err
	}
	jsonOutput := false
	comments := false
	for _, argument := range arguments[1:] {
		switch argument {
		case "--json":
			jsonOutput = true
		case "--comments":
			comments = true
		default:
			return fmt.Errorf("unknown issue get option %q", argument)
		}
	}
	command := []string{"view", strconv.Itoa(number)}
	if jsonOutput {
		fields := "number,title,body,state,labels,assignees,author,url,createdAt,updatedAt"
		if comments {
			fields += ",comments"
		}
		command = append(command, "--json", fields)
	} else if comments {
		command = append(command, "--comments")
	}
	return a.writeIssueGH(nil, command...)
}

func parseIssueEdit(arguments []string) (issueEditOptions, error) {
	var options issueEditOptions
	if len(arguments) == 0 {
		return options, errors.New("issue edit requires one issue number")
	}
	number, err := parseIssueNumber(arguments[0])
	if err != nil {
		return options, err
	}
	options.number = number
	options.fields = make(map[string]string)
	for index := 1; index < len(arguments); index++ {
		name, value, consumed, err := issueOption(arguments, index)
		if err != nil {
			return options, err
		}
		index += consumed
		switch name {
		case "--title":
			options.title, options.titleSet = value, true
		case "--field":
			if err := addIssueField(options.fields, value); err != nil {
				return options, err
			}
		case "--add-label":
			options.addLabels = append(options.addLabels, value)
		case "--remove-label":
			options.removeLabels = append(options.removeLabels, value)
		case "--add-assignee":
			options.addAssignees = append(options.addAssignees, value)
		case "--remove-assignee":
			options.removeAssignees = append(options.removeAssignees, value)
		case "--dry-run":
			options.dryRun = true
		default:
			return options, fmt.Errorf("unknown issue edit option %q", name)
		}
	}
	if options.titleSet {
		if err := validIssueMetadata("title", options.title); err != nil {
			return options, err
		}
	}
	if !options.titleSet && len(options.fields) == 0 && len(options.addLabels) == 0 && len(options.removeLabels) == 0 && len(options.addAssignees) == 0 && len(options.removeAssignees) == 0 {
		return options, errors.New("issue edit requires at least one change")
	}
	for name, values := range map[string][]string{"label": slices.Concat(options.addLabels, options.removeLabels), "assignee": slices.Concat(options.addAssignees, options.removeAssignees)} {
		if err := validateIssueValues(name, values); err != nil {
			return options, err
		}
	}
	return options, nil
}

func (a *app) editIssue(options issueEditOptions) error {
	if !options.dryRun || len(options.fields) != 0 {
		if err := a.ensureGH(); err != nil {
			return err
		}
	}
	arguments := []string{"edit", strconv.Itoa(options.number)}
	var body string
	if len(options.fields) != 0 {
		output, err := a.runIssueGHAuthenticated(nil, "view", strconv.Itoa(options.number), "--json", "body")
		if err != nil {
			return err
		}
		var issue struct {
			Body string `json:"body"`
		}
		if err := json.Unmarshal(output, &issue); err != nil {
			return fmt.Errorf("decode issue %d: %w", options.number, err)
		}
		body, err = updateManagedIssue(issue.Body, options.fields)
		if err != nil {
			return fmt.Errorf("edit issue %d: %w", options.number, err)
		}
		arguments = append(arguments, "--body-file", "-")
	}
	if options.titleSet {
		arguments = append(arguments, "--title", options.title)
	}
	for _, value := range options.addLabels {
		arguments = append(arguments, "--add-label", value)
	}
	for _, value := range options.removeLabels {
		arguments = append(arguments, "--remove-label", value)
	}
	for _, value := range options.addAssignees {
		arguments = append(arguments, "--add-assignee", value)
	}
	for _, value := range options.removeAssignees {
		arguments = append(arguments, "--remove-assignee", value)
	}
	if options.dryRun {
		fmt.Fprintf(a.stdout, "Repository: %s\nIssue: %d\n", godoIssueRepository, options.number)
		if options.titleSet {
			fmt.Fprintf(a.stdout, "Title: %s\n", options.title)
		}
		if body != "" {
			fmt.Fprintf(a.stdout, "\n%s", body)
		}
		for _, value := range options.addLabels {
			fmt.Fprintf(a.stdout, "Add label: %s\n", value)
		}
		for _, value := range options.removeLabels {
			fmt.Fprintf(a.stdout, "Remove label: %s\n", value)
		}
		for _, value := range options.addAssignees {
			fmt.Fprintf(a.stdout, "Add assignee: %s\n", value)
		}
		for _, value := range options.removeAssignees {
			fmt.Fprintf(a.stdout, "Remove assignee: %s\n", value)
		}
		return nil
	}
	var input io.Reader
	if body != "" {
		input = strings.NewReader(body)
	}
	output, err := a.runIssueGHAuthenticated(input, arguments...)
	if err != nil {
		return err
	}
	_, err = a.stdout.Write(output)
	return err
}

func (a *app) commentIssue(arguments []string) error {
	if len(arguments) != 3 || arguments[1] != "--body" {
		return errors.New("issue comment requires <number> --body <text>")
	}
	number, err := parseIssueNumber(arguments[0])
	if err != nil {
		return err
	}
	if err := validIssueText("comment", arguments[2], true); err != nil {
		return err
	}
	return a.writeIssueGH(strings.NewReader(arguments[2]), "comment", strconv.Itoa(number), "--body-file", "-")
}

func (a *app) changeIssueState(state string, arguments []string) error {
	if len(arguments) < 1 {
		return fmt.Errorf("issue %s requires one issue number", state)
	}
	number, err := parseIssueNumber(arguments[0])
	if err != nil {
		return err
	}
	command := []string{state, strconv.Itoa(number)}
	if len(arguments) != 1 {
		if len(arguments) != 3 || arguments[1] != "--comment" {
			return fmt.Errorf("issue %s accepts only --comment <text>", state)
		}
		if err := validIssueText("comment", arguments[2], true); err != nil {
			return err
		}
		command = append(command, "--comment", arguments[2])
	}
	return a.writeIssueGH(nil, command...)
}

func (a *app) runIssueGH(input io.Reader, arguments ...string) ([]byte, error) {
	if err := a.ensureGH(); err != nil {
		return nil, err
	}
	return a.runIssueGHAuthenticated(input, arguments...)
}

func (a *app) runIssueGHAuthenticated(input io.Reader, arguments ...string) ([]byte, error) {
	arguments = append([]string{"issue"}, arguments...)
	arguments = append(arguments, "--repo", godoIssueRepository)
	output, err := a.gh()(input, arguments...)
	if err != nil {
		return nil, fmt.Errorf("GitHub issue operation failed: %w", err)
	}
	return output, nil
}

func (a *app) writeIssueGH(input io.Reader, arguments ...string) error {
	output, err := a.runIssueGH(input, arguments...)
	if err != nil {
		return err
	}
	_, err = a.stdout.Write(output)
	return err
}

func (a *app) ensureGH() error {
	if a.runGH == nil {
		if _, err := exec.LookPath("gh"); err != nil {
			return errors.New("GitHub issues require the gh CLI; install it from https://cli.github.com and run \"gh auth login\"")
		}
	}
	if _, err := a.gh()(nil, "auth", "status", "--hostname", "github.com"); err != nil {
		return fmt.Errorf("GitHub CLI is not authenticated for github.com; run \"gh auth login\": %w", err)
	}
	return nil
}

func (a *app) gh() ghRunner {
	if a.runGH != nil {
		return a.runGH
	}
	return executeGH
}

func executeGH(input io.Reader, arguments ...string) ([]byte, error) {
	command := exec.Command("gh", arguments...)
	command.Stdin = input
	var output, diagnostics bytes.Buffer
	command.Stdout = &output
	command.Stderr = &diagnostics
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(diagnostics.String())
		if message == "" {
			message = err.Error()
		}
		return nil, errors.New(message)
	}
	return output.Bytes(), nil
}

func renderIssueBody(template issueTemplate, values map[string]string) (string, error) {
	if err := validateTemplateFields(template, values, true); err != nil {
		return "", err
	}
	var result strings.Builder
	fmt.Fprintf(&result, "<!-- godo:issue template=%s version=1 -->\n%s\n", template.ID, managedStart)
	result.WriteString(renderManagedFields(template, values))
	result.WriteString(managedEnd + "\n")
	if result.Len() > maxIssueBodyBytes {
		return "", errors.New("rendered issue body exceeds 65000 bytes")
	}
	return result.String(), nil
}

func renderManagedFields(template issueTemplate, values map[string]string) string {
	var result strings.Builder
	for _, field := range template.Fields {
		value, exists := values[field.Name]
		if !exists || value == "" && !field.Required {
			continue
		}
		fmt.Fprintf(&result, "\n## %s\n\n<!-- godo:field:%s:start -->\n%s\n<!-- godo:field:%s:end -->\n", field.Heading, field.Name, value, field.Name)
	}
	return result.String()
}

func updateManagedIssue(body string, changes map[string]string) (string, error) {
	matches := issueMarkerPattern.FindAllStringSubmatch(body, -1)
	if len(matches) != 1 || strings.Count(body, managedStart) != 1 || strings.Count(body, managedEnd) != 1 {
		return "", errors.New("body does not contain one unambiguous godo managed block")
	}
	match := matches[0]
	if len(match) != 3 || match[2] != "1" {
		return "", errors.New("body is not a supported godo-managed issue")
	}
	template, ok := findIssueTemplate(match[1])
	if !ok {
		return "", fmt.Errorf("body uses unknown godo issue template %q", match[1])
	}
	start := strings.Index(body, managedStart)
	end := strings.Index(body, managedEnd)
	if start < 0 || end < start {
		return "", errors.New("body has invalid godo managed markers")
	}
	managed := body[start:end]
	values := make(map[string]string)
	for _, field := range template.Fields {
		fieldStart := "<!-- godo:field:" + field.Name + ":start -->"
		fieldEnd := "<!-- godo:field:" + field.Name + ":end -->"
		if strings.Count(managed, fieldStart) > 1 || strings.Count(managed, fieldEnd) > 1 {
			return "", fmt.Errorf("body has duplicate markers for field %q", field.Name)
		}
		from := strings.Index(managed, fieldStart)
		to := strings.Index(managed, fieldEnd)
		if from < 0 && to < 0 {
			continue
		}
		if from < 0 || to < from {
			return "", fmt.Errorf("body has invalid markers for field %q", field.Name)
		}
		from += len(fieldStart)
		values[field.Name] = strings.Trim(managed[from:to], "\n")
	}
	if err := validateTemplateFields(template, changes, false); err != nil {
		return "", err
	}
	for name, value := range changes {
		values[name] = value
	}
	if err := validateTemplateFields(template, values, true); err != nil {
		return "", err
	}
	replacement := managedStart + "\n" + renderManagedFields(template, values) + managedEnd
	updated := body[:start] + replacement + body[end+len(managedEnd):]
	if len(updated) > maxIssueBodyBytes {
		return "", errors.New("updated issue body exceeds 65000 bytes")
	}
	return updated, nil
}

func validateTemplateFields(template issueTemplate, values map[string]string, requireAll bool) error {
	known := make(map[string]issueField, len(template.Fields))
	for _, field := range template.Fields {
		known[field.Name] = field
		if requireAll && field.Required && strings.TrimSpace(values[field.Name]) == "" {
			return fmt.Errorf("template %s requires --field %s=<value>", template.ID, field.Name)
		}
	}
	for name, value := range values {
		if _, ok := known[name]; !ok {
			return fmt.Errorf("template %s has no field %q", template.ID, name)
		}
		if err := validIssueText("field "+name, value, false); err != nil {
			return err
		}
		if strings.Contains(value, "<!-- godo:") {
			return fmt.Errorf("field %s contains a reserved godo marker", name)
		}
	}
	return nil
}

func addIssueField(fields map[string]string, value string) error {
	name, fieldValue, found := strings.Cut(value, "=")
	if !found || name == "" {
		return fmt.Errorf("invalid issue field %q: expected name=value", value)
	}
	if _, exists := fields[name]; exists {
		return fmt.Errorf("issue field %q was repeated", name)
	}
	fields[name] = fieldValue
	return nil
}

func issueOption(arguments []string, index int) (string, string, int, error) {
	argument := arguments[index]
	name, value, inline := strings.Cut(argument, "=")
	switch name {
	case "--dry-run", "--json":
		if inline {
			return "", "", 0, fmt.Errorf("option %s does not accept a value", name)
		}
		return name, "", 0, nil
	}
	if !strings.HasPrefix(name, "--") {
		return "", "", 0, fmt.Errorf("unexpected argument %q", argument)
	}
	if inline {
		return name, value, 0, nil
	}
	if index+1 >= len(arguments) {
		return "", "", 0, fmt.Errorf("%s requires a value", name)
	}
	return name, arguments[index+1], 1, nil
}

func findIssueTemplate(name string) (issueTemplate, bool) {
	for _, template := range issueTemplates {
		if template.ID == name {
			return template, true
		}
	}
	return issueTemplate{}, false
}

func unknownIssueTemplate(name string) error {
	return fmt.Errorf("unknown issue template %q; run godo issue templates", name)
}

func parseIssueNumber(value string) (int, error) {
	number, err := strconv.Atoi(value)
	if err != nil || number < 1 {
		return 0, fmt.Errorf("invalid issue number %q", value)
	}
	return number, nil
}

func issueNumberFromURL(value string) (int, error) {
	prefix := "https://github.com/" + godoIssueRepository + "/issues/"
	if !strings.HasPrefix(value, prefix) {
		return 0, fmt.Errorf("gh returned an unexpected issue URL %q", value)
	}
	return parseIssueNumber(strings.TrimPrefix(value, prefix))
}

func parseOnlyJSON(arguments []string) (bool, error) {
	if len(arguments) == 0 {
		return false, nil
	}
	if len(arguments) == 1 && arguments[0] == "--json" {
		return true, nil
	}
	return false, fmt.Errorf("unexpected option %q", arguments[0])
}

func validateIssueValues(name string, values []string) error {
	for _, value := range values {
		if err := validIssueMetadata(name, value); err != nil {
			return err
		}
	}
	return nil
}

func validIssueMetadata(name, value string) error {
	if err := validIssueText(name, value, true); err != nil {
		return err
	}
	if strings.ContainsAny(value, "\r\n\t") {
		return fmt.Errorf("issue %s must be one line", name)
	}
	return nil
}

func validIssueText(name, value string, required bool) error {
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("issue %s must not be empty", name)
	}
	for _, character := range value {
		if character == 0 || character == '\r' || character < 0x20 && character != '\n' && character != '\t' {
			return fmt.Errorf("issue %s contains invalid control characters", name)
		}
	}
	return nil
}

func compactStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !slices.Contains(result, value) {
			result = append(result, value)
		}
	}
	return result
}

func writeJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}
