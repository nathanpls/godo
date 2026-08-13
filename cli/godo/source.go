package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const godoModulePath = "github.com/nathanpls/godo"

type sourceLocation struct {
	moduleDir string
	targetDir string
}

type sourceSearchOptions struct {
	query       string
	packagePath string
	context     int
}

type sourceModule struct {
	Path string
	Dir  string
}

type sourcePackage struct {
	Dir string
}

type sourceMatch struct {
	line int
}

func (a *app) runSource(arguments []string) error {
	if len(arguments) == 0 || isHelp(arguments) {
		return printHelp(a.stdout, sourceHelp)
	}
	if arguments[0] == "search" {
		if isHelp(arguments[1:]) {
			return printHelp(a.stdout, sourceSearchHelp)
		}
		options, err := parseSourceSearch(arguments[1:])
		if err != nil {
			return err
		}
		return a.searchSource(options)
	}
	if len(arguments) != 1 {
		return errors.New("source requires one package; run godo source --help")
	}
	if err := validateSourcePackage(arguments[0]); err != nil {
		return err
	}
	location, err := a.sourceLocation(arguments[0])
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(a.stdout, location.targetDir)
	return err
}

func parseSourceSearch(arguments []string) (sourceSearchOptions, error) {
	var options sourceSearchOptions
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch {
		case argument == "--package":
			index++
			if index == len(arguments) {
				return options, errors.New("--package requires a value")
			}
			options.packagePath = arguments[index]
		case strings.HasPrefix(argument, "--package="):
			options.packagePath = strings.TrimPrefix(argument, "--package=")
		case argument == "--context":
			index++
			if index == len(arguments) {
				return options, errors.New("--context requires a value")
			}
			context, err := parseSourceContext(arguments[index])
			if err != nil {
				return options, err
			}
			options.context = context
		case strings.HasPrefix(argument, "--context="):
			context, err := parseSourceContext(strings.TrimPrefix(argument, "--context="))
			if err != nil {
				return options, err
			}
			options.context = context
		case strings.HasPrefix(argument, "-"):
			return options, fmt.Errorf("unknown source search option %q", argument)
		case options.query == "":
			options.query = argument
		default:
			return options, errors.New("source search requires one query; run godo source search --help")
		}
	}
	if options.query == "" {
		return options, errors.New("source search requires one query; run godo source search --help")
	}
	if options.packagePath != "" {
		if err := validateSourcePackage(options.packagePath); err != nil {
			return options, err
		}
	}
	return options, nil
}

func parseSourceContext(value string) (int, error) {
	context, err := strconv.Atoi(value)
	if err != nil || context < 0 {
		return 0, fmt.Errorf("invalid context %q: expected a non-negative integer", value)
	}
	return context, nil
}

func validateSourcePackage(packagePath string) error {
	if packagePath == "" {
		return errors.New("source package cannot be empty")
	}
	if packagePath == "." {
		return nil
	}
	if strings.Contains(packagePath, `\`) || strings.HasPrefix(packagePath, "/") || path.Clean(packagePath) != packagePath || packagePath == ".." || strings.HasPrefix(packagePath, "../") {
		return fmt.Errorf("invalid godo package %q", packagePath)
	}
	return nil
}

func (a *app) searchSource(options sourceSearchOptions) error {
	location, err := a.sourceLocation(options.packagePath)
	if err != nil {
		return err
	}
	return searchSource(a.stdout, location.moduleDir, location.targetDir, options.query, options.context)
}

func (a *app) sourceLocation(packagePath string) (sourceLocation, error) {
	root, err := projectRoot(a.cwd)
	if err != nil {
		return sourceLocation{}, err
	}
	resolve := a.resolveSource
	if resolve == nil {
		resolve = resolveGodoSource
	}
	return resolve(root, packagePath)
}

func resolveGodoSource(root, packagePath string) (sourceLocation, error) {
	var module sourceModule
	if err := runGoList(root, &module, "-m", "-json", godoModulePath); err != nil {
		return sourceLocation{}, fmt.Errorf("resolve godo module: %w", err)
	}
	if module.Path != godoModulePath || module.Dir == "" {
		return sourceLocation{}, errors.New("resolve godo module: Go did not return its source directory")
	}

	location := sourceLocation{moduleDir: module.Dir, targetDir: module.Dir}
	if packagePath == "" || packagePath == "." {
		return location, nil
	}
	var packageInfo sourcePackage
	if err := runGoList(root, &packageInfo, "-json", godoModulePath+"/"+packagePath); err != nil {
		return sourceLocation{}, fmt.Errorf("resolve godo package %q: %w", packagePath, err)
	}
	if packageInfo.Dir == "" || !pathWithin(module.Dir, packageInfo.Dir) {
		return sourceLocation{}, fmt.Errorf("resolved package %q is outside the godo module", packagePath)
	}
	location.targetDir = packageInfo.Dir
	return location, nil
}

func runGoList(root string, result any, arguments ...string) error {
	command := exec.Command("go", append([]string{"list"}, arguments...)...)
	command.Dir = root
	var output bytes.Buffer
	var diagnostics bytes.Buffer
	command.Stdout = &output
	command.Stderr = &diagnostics
	if err := command.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return errors.New("go executable not found")
		}
		message := strings.TrimSpace(diagnostics.String())
		if message != "" {
			return errors.New(message)
		}
		return err
	}
	decoder := json.NewDecoder(&output)
	if err := decoder.Decode(result); err != nil {
		return fmt.Errorf("decode go list output: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("go list produced unexpected output")
	}
	return nil
}

func pathWithin(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func searchSource(output io.Writer, moduleDir, targetDir, query string, context int) error {
	var files []string
	err := filepath.WalkDir(targetDir, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath != targetDir && entry.IsDir() && strings.HasPrefix(entry.Name(), ".") {
			return filepath.SkipDir
		}
		if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".go") {
			files = append(files, filePath)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk godo source: %w", err)
	}
	slices.Sort(files)
	firstFile := true
	for _, filePath := range files {
		content, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("read %s: %w", filePath, err)
		}
		lines := strings.Split(string(content), "\n")
		if lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		for index := range lines {
			lines[index] = strings.TrimSuffix(lines[index], "\r")
		}
		matches := sourceMatches(lines, query)
		if len(matches) == 0 {
			continue
		}
		relative, err := filepath.Rel(moduleDir, filePath)
		if err != nil {
			return fmt.Errorf("make source path relative: %w", err)
		}
		relative = filepath.ToSlash(relative)
		if context == 0 {
			for _, match := range matches {
				if _, err := fmt.Fprintf(output, "%s:%d:%s\n", relative, match.line+1, lines[match.line]); err != nil {
					return err
				}
			}
			continue
		}
		groups := sourceContextGroups(matches, len(lines), context)
		matchedLines := make(map[int]bool, len(matches))
		for _, match := range matches {
			matchedLines[match.line] = true
		}
		if !firstFile {
			if _, err := fmt.Fprintln(output); err != nil {
				return err
			}
		}
		firstFile = false
		if _, err := fmt.Fprintf(output, "[%s]\n", relative); err != nil {
			return err
		}
		lineWidth := len(strconv.Itoa(groups[len(groups)-1][1] + 1))
		for index, group := range groups {
			if index > 0 {
				if _, err := fmt.Fprintln(output, "..."); err != nil {
					return err
				}
			}
			for line := group[0]; line <= group[1]; line++ {
				marker := ' '
				if matchedLines[line] {
					marker = '>'
				}
				if _, err := fmt.Fprintf(output, "%c %*d %s\n", marker, lineWidth, line+1, lines[line]); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func sourceMatches(lines []string, query string) []sourceMatch {
	var matches []sourceMatch
	for line, content := range lines {
		if strings.Contains(content, query) {
			matches = append(matches, sourceMatch{line: line})
		}
	}
	return matches
}

func sourceContextGroups(matches []sourceMatch, lineCount, context int) [][2]int {
	groups := make([][2]int, 0, len(matches))
	for _, match := range matches {
		start := max(0, match.line-context)
		end := min(lineCount-1, match.line+context)
		if len(groups) > 0 && start <= groups[len(groups)-1][1]+1 {
			groups[len(groups)-1][1] = max(groups[len(groups)-1][1], end)
			continue
		}
		groups = append(groups, [2]int{start, end})
	}
	return groups
}
