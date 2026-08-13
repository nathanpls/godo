package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const projectMain = `package main

func main() {}
`

const projectGitignore = `# Go build and test output
bin/
*.out
*.test

# Local environment
.env
`

type projectInitOptions struct {
	directory string
	module    string
}

func parseProjectInit(arguments []string) (projectInitOptions, error) {
	options := projectInitOptions{directory: "."}
	directorySet := false
	for i := 0; i < len(arguments); i++ {
		argument := arguments[i]
		name, inline, hasInline := strings.Cut(argument, "=")
		if name == "--module" {
			value := inline
			if !hasInline {
				i++
				if i >= len(arguments) {
					return projectInitOptions{}, errors.New("--module requires a value")
				}
				value = arguments[i]
			}
			options.module = value
			continue
		}
		if strings.HasPrefix(argument, "-") {
			return projectInitOptions{}, fmt.Errorf("unknown init option %q", argument)
		}
		if directorySet {
			return projectInitOptions{}, errors.New("init accepts one directory")
		}
		options.directory = argument
		directorySet = true
	}
	if options.directory == "" {
		return projectInitOptions{}, errors.New("project directory must not be empty")
	}
	if strings.ContainsAny(options.module, "\r\n") {
		return projectInitOptions{}, errors.New("module path must not contain newlines")
	}
	return options, nil
}

func (a *app) initProject(options projectInitOptions) error {
	target := options.directory
	if !filepath.IsAbs(target) {
		target = filepath.Join(a.cwd, target)
	}
	target, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve project directory: %w", err)
	}
	module := options.module
	if module == "" {
		module = filepath.Base(target)
	}
	if module == "." || module == string(filepath.Separator) || strings.TrimSpace(module) == "" {
		return errors.New("could not infer a module path; use --module <path>")
	}
	if err := validateModulePath(module); err != nil {
		return err
	}

	entries, err := os.ReadDir(target)
	createdDirectory := false
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(target, 0o755); err != nil {
			return fmt.Errorf("create project directory: %w", err)
		}
		createdDirectory = true
		entries = nil
	} else if err != nil {
		return fmt.Errorf("read project directory: %w", err)
	}
	if len(entries) != 0 {
		return fmt.Errorf("project directory %s is not empty", target)
	}

	files := []struct {
		name    string
		content string
	}{
		{"go.mod", "module " + module + "\n\ngo 1.26.0\n"},
		{"main.go", projectMain},
		{".gitignore", projectGitignore},
	}
	created := make([]string, 0, len(files))
	for _, file := range files {
		path := filepath.Join(target, file.name)
		if err := writeNewFile(path, []byte(file.content), 0o644); err != nil {
			for _, createdPath := range created {
				_ = os.Remove(createdPath)
			}
			if createdDirectory {
				_ = os.Remove(target)
			}
			return fmt.Errorf("create %s: %w", file.name, err)
		}
		created = append(created, path)
	}

	fmt.Fprintf(a.stdout, "Initialized Go project %s in %s\n", module, target)
	fmt.Fprintln(a.stdout, "Next:")
	fmt.Fprintln(a.stdout, "  godo add http")
	return nil
}

func validateModulePath(module string) error {
	if strings.ContainsAny(module, " \t\\") || strings.HasPrefix(module, "/") || strings.HasSuffix(module, "/") || strings.Contains(module, "//") {
		return fmt.Errorf("invalid module path %q", module)
	}
	for _, part := range strings.Split(module, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("invalid module path %q", module)
		}
	}
	return nil
}

func writeNewFile(path string, content []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		_ = os.Remove(path)
		return err
	}
	return file.Close()
}

type dependency struct {
	path string
	docs string
	next string
}

var dependencies = map[string]dependency{
	"http": {
		path: "github.com/nathanpls/godo/http",
		docs: "http://localhost:41000/http",
	},
	"orm": {
		path: "github.com/nathanpls/godo/orm",
		docs: "http://localhost:41000/orm",
	},
	"ratelimit": {
		path: "github.com/nathanpls/godo/http/plugins/ratelimit",
		docs: "http://localhost:41000/http/plugins/ratelimit",
	},
	"sqlite": {
		path: "modernc.org/sqlite",
		docs: "http://localhost:41000/orm",
		next: "godo db init --dialect sqlite",
	},
	"postgres": {
		path: "github.com/jackc/pgx/v5/stdlib",
		docs: "http://localhost:41000/orm",
		next: "godo db init --dialect postgres",
	},
}

func (a *app) addDependency(arguments []string) error {
	if len(arguments) != 1 {
		return errors.New("add requires one package name; run godo add --help")
	}
	selected, exists := dependencies[arguments[0]]
	if !exists {
		return fmt.Errorf("unknown package %q; run godo add --help", arguments[0])
	}
	root, err := projectRoot(a.cwd)
	if err != nil {
		return err
	}
	goGet := a.goGet
	if goGet == nil {
		goGet = runGoGet
	}
	if err := goGet(root, selected.path); err != nil {
		return fmt.Errorf("add %s: %w", arguments[0], err)
	}
	fmt.Fprintf(a.stdout, "Added %s (%s)\n", arguments[0], selected.path)
	if selected.docs != "" {
		fmt.Fprintf(a.stdout, "Docs: %s\n", selected.docs)
	}
	if selected.next != "" {
		fmt.Fprintf(a.stdout, "Next: %s\n", selected.next)
	}
	return nil
}

func runGoGet(root, dependency string) error {
	command := exec.Command("go", "get", dependency)
	command.Dir = root
	command.Stdout = os.Stderr
	command.Stderr = os.Stderr
	return command.Run()
}
