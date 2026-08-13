package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
)

type app struct {
	store         store
	supervisor    supervisor
	agentsFile    string
	stdout        io.Writer
	cwd           string
	goGet         func(string, string) error
	resolveSource func(string, string) (sourceLocation, error)
	runGH         ghRunner
}

func newApp() (*app, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("find config directory: %w", err)
	}
	dataDir, err := userDataDir()
	if err != nil {
		return nil, fmt.Errorf("find data directory: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("find working directory: %w", err)
	}
	return &app{
		store: store{
			configDir: filepath.Join(configDir, "godo"),
			dataDir:   filepath.Join(dataDir, "godo"),
		},
		supervisor:    systemdSupervisor{unitDir: filepath.Join(configDir, "systemd", "user")},
		agentsFile:    filepath.Join(configDir, "opencode", "AGENTS.md"),
		stdout:        os.Stdout,
		cwd:           cwd,
		goGet:         runGoGet,
		resolveSource: resolveGodoSource,
	}, nil
}

func userDataDir() (string, error) {
	if directory := os.Getenv("XDG_DATA_HOME"); directory != "" {
		return directory, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share"), nil
}

func (a *app) run(args []string) error {
	if len(args) == 0 || isHelp(args) {
		return printHelp(a.stdout, rootHelp)
	}

	switch args[0] {
	case "init":
		if isHelp(args[1:]) {
			return printHelp(a.stdout, initHelp)
		}
		options, err := parseProjectInit(args[1:])
		if err != nil {
			return err
		}
		return a.initProject(options)
	case "add":
		if isHelp(args[1:]) {
			return printHelp(a.stdout, addHelp)
		}
		return a.addDependency(args[1:])
	case "auth":
		return a.runAuth(args[1:])
	case "api":
		return a.runAPI(args[1:])
	case "issue":
		return a.runIssue(args[1:])
	case "service":
		return a.runService(args[1:])
	case "db":
		return a.runDB(args[1:])
	case "source":
		return a.runSource(args[1:])
	case "agent":
		if isHelp(args[1:]) {
			return printHelp(a.stdout, agentHelp)
		}
		if len(args) != 1 {
			return errors.New("agent does not accept arguments")
		}
		return a.syncAgentsAndPrint()
	default:
		return fmt.Errorf("unknown command %q; run godo --help", args[0])
	}
}

func (a *app) runService(args []string) error {
	if len(args) == 0 || isHelp(args) {
		return printHelp(a.stdout, serviceHelp)
	}
	switch args[0] {
	case "add":
		if isHelp(args[1:]) {
			return printHelp(a.stdout, serviceAddHelp)
		}
		options, err := parseAdd(args[1:])
		if err != nil {
			return err
		}
		return a.add(options)
	case "list", "ls":
		if isHelp(args[1:]) {
			return printHelp(a.stdout, serviceListHelp)
		}
		if len(args) != 1 {
			return errors.New("service list does not accept arguments")
		}
		return a.list()
	case "update":
		if isHelp(args[1:]) {
			return printHelp(a.stdout, serviceUpdateHelp)
		}
		id, err := parseID(args[1:])
		if err != nil {
			return fmt.Errorf("service update: %w", err)
		}
		return a.update(id)
	case "restart":
		if isHelp(args[1:]) {
			return printHelp(a.stdout, serviceRestartHelp)
		}
		id, err := parseID(args[1:])
		if err != nil {
			return fmt.Errorf("service restart: %w", err)
		}
		return a.restart(id)
	case "edit":
		if isHelp(args[1:]) {
			return printHelp(a.stdout, serviceEditHelp)
		}
		options, err := parseServiceEdit(args[1:])
		if err != nil {
			return err
		}
		return a.edit(options)
	case "remove", "rm":
		if isHelp(args[1:]) {
			return printHelp(a.stdout, serviceRemoveHelp)
		}
		id, err := parseID(args[1:])
		if err != nil {
			return fmt.Errorf("service remove: %w", err)
		}
		return a.remove(id)
	default:
		return fmt.Errorf("unknown service command %q", args[0])
	}
}

type serviceEditOptions struct {
	id           int
	name         string
	additions    string
	workDir      string
	envFile      string
	args         []string
	noAgent      bool
	nameSet      bool
	additionsSet bool
	workDirSet   bool
	envFileSet   bool
	argsSet      bool
	agentSet     bool
}

func parseServiceEdit(arguments []string) (serviceEditOptions, error) {
	var options serviceEditOptions
	for i := 0; i < len(arguments); i++ {
		argument := arguments[i]
		if argument == "--" {
			options.args = append([]string(nil), arguments[i+1:]...)
			options.argsSet = true
			break
		}
		name, inline, hasInline := strings.Cut(argument, "=")
		switch name {
		case "--name", "--additions", "--workdir", "--env-file":
			value := inline
			if !hasInline {
				i++
				if i >= len(arguments) {
					return serviceEditOptions{}, fmt.Errorf("%s requires a value", name)
				}
				value = arguments[i]
			}
			switch name {
			case "--name":
				options.name, options.nameSet = value, true
			case "--additions":
				options.additions, options.additionsSet = value, true
			case "--workdir":
				options.workDir, options.workDirSet = value, true
			case "--env-file":
				options.envFile, options.envFileSet = value, true
			}
		case "--agent", "--no-agent":
			if hasInline {
				return serviceEditOptions{}, fmt.Errorf("%s does not accept a value", name)
			}
			if options.agentSet {
				return serviceEditOptions{}, errors.New("service edit accepts only one of --agent or --no-agent")
			}
			options.noAgent, options.agentSet = name == "--no-agent", true
		default:
			if strings.HasPrefix(argument, "-") {
				return serviceEditOptions{}, fmt.Errorf("unknown service edit option %q", argument)
			}
			if options.id != 0 {
				return serviceEditOptions{}, errors.New("service edit accepts one service ID")
			}
			id, err := strconv.Atoi(argument)
			if err != nil || id < 1 {
				return serviceEditOptions{}, fmt.Errorf("invalid service ID %q", argument)
			}
			options.id = id
		}
	}
	if options.id == 0 {
		return serviceEditOptions{}, errors.New("service edit requires a service ID")
	}
	if !options.nameSet && !options.additionsSet && !options.workDirSet && !options.envFileSet && !options.argsSet && !options.agentSet {
		return serviceEditOptions{}, errors.New("service edit requires a change")
	}
	if options.nameSet && strings.TrimSpace(options.name) == "" {
		return serviceEditOptions{}, errors.New("service name must not be empty")
	}
	if strings.ContainsAny(options.name, "\r\n|") {
		return serviceEditOptions{}, errors.New("service name must not contain newlines or pipes")
	}
	for _, argument := range options.args {
		if strings.ContainsRune(argument, 0) || strings.ContainsAny(argument, "\r\n") {
			return serviceEditOptions{}, errors.New("service arguments must not contain NUL bytes or newlines")
		}
	}
	return options, nil
}

type addOptions struct {
	target    string
	name      string
	additions string
	workDir   string
	envFile   string
	args      []string
	noAgent   bool
	port      int
}

func parseAdd(args []string) (addOptions, error) {
	var options addOptions
	for i := 0; i < len(args); i++ {
		argument := args[i]
		if argument == "--" {
			options.args = append([]string(nil), args[i+1:]...)
			break
		}
		name, inlineValue, hasInlineValue := strings.Cut(argument, "=")
		switch name {
		case "--name", "--port", "--additions", "--workdir", "--env-file":
			value := inlineValue
			if !hasInlineValue {
				i++
				if i >= len(args) {
					return addOptions{}, fmt.Errorf("%s requires a value", name)
				}
				value = args[i]
			}
			switch name {
			case "--name":
				options.name = value
			case "--additions":
				options.additions = value
			case "--workdir":
				options.workDir = value
			case "--env-file":
				options.envFile = value
			case "--port":
				port, err := strconv.Atoi(value)
				if err != nil || port < 1 || port > 65535 {
					return addOptions{}, fmt.Errorf("invalid port %q", value)
				}
				options.port = port
			}
		case "--no-agent":
			if hasInlineValue {
				return addOptions{}, errors.New("--no-agent does not accept a value")
			}
			options.noAgent = true
		default:
			if strings.HasPrefix(argument, "-") {
				return addOptions{}, fmt.Errorf("unknown option %q", argument)
			}
			if options.target != "" {
				return addOptions{}, errors.New("service add accepts one target")
			}
			options.target = argument
		}
	}
	if options.target == "" {
		return addOptions{}, errors.New("service add requires a target")
	}
	if strings.ContainsAny(options.name, "\r\n|") {
		return addOptions{}, errors.New("service name must not contain newlines or pipes")
	}
	for _, argument := range options.args {
		if strings.ContainsRune(argument, 0) || strings.ContainsAny(argument, "\r\n") {
			return addOptions{}, errors.New("service arguments must not contain NUL bytes or newlines")
		}
	}
	return options, nil
}

func parseID(args []string) (int, error) {
	if len(args) != 1 {
		return 0, errors.New("requires one service ID")
	}
	id, err := strconv.Atoi(args[0])
	if err != nil || id < 1 {
		return 0, fmt.Errorf("invalid service ID %q", args[0])
	}
	return id, nil
}

type resolvedTarget struct {
	kind        string
	buildDir    string
	target      string
	workDir     string
	defaultName string
}

func resolveTarget(cwd, target string) (resolvedTarget, error) {
	path := target
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	info, err := os.Stat(path)
	if err == nil {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return resolvedTarget{}, err
		}
		if info.IsDir() {
			return resolvedTarget{kind: "go", buildDir: absolute, target: ".", workDir: absolute, defaultName: filepath.Base(absolute)}, nil
		}
		if !info.Mode().IsRegular() {
			return resolvedTarget{}, errors.New("service target must be a directory, Go file, package path, or executable file")
		}
		if filepath.Ext(absolute) == ".go" {
			name := strings.TrimSuffix(filepath.Base(absolute), ".go")
			return resolvedTarget{kind: "go", buildDir: filepath.Dir(absolute), target: filepath.Base(absolute), workDir: filepath.Dir(absolute), defaultName: name}, nil
		}
		if info.Mode().Perm()&0o111 == 0 {
			return resolvedTarget{}, errors.New("service executable target must have an executable bit")
		}
		return resolvedTarget{kind: "executable", buildDir: filepath.Dir(absolute), target: absolute, workDir: cwd, defaultName: filepath.Base(absolute)}, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return resolvedTarget{}, fmt.Errorf("inspect target: %w", err)
	}
	if strings.HasPrefix(target, ".") || filepath.IsAbs(target) || strings.HasSuffix(target, ".go") {
		return resolvedTarget{}, fmt.Errorf("target %q does not exist", target)
	}
	name := filepath.Base(target)
	return resolvedTarget{kind: "go", buildDir: cwd, target: target, workDir: cwd, defaultName: name}, nil
}

func choosePort(requested int, services []service) (int, error) {
	used := make(map[int]bool, len(services))
	for _, service := range services {
		used[service.Port] = true
	}
	if requested != 0 {
		if used[requested] {
			return 0, fmt.Errorf("port %d is already assigned to a godo service", requested)
		}
		if !portAvailable(requested) {
			return 0, fmt.Errorf("port %d is not available", requested)
		}
		return requested, nil
	}
	for port := 41000; port <= 41999; port++ {
		if !used[port] && portAvailable(port) {
			return port, nil
		}
	}
	return 0, errors.New("no free port in the godo range 41000-41999")
}

func portAvailable(port int) bool {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	return listener.Close() == nil
}

func (a *app) list() error {
	value, err := a.store.load()
	if err != nil {
		return err
	}
	if len(value.Services) == 0 {
		fmt.Fprintln(a.stdout, "No services.")
		return nil
	}

	writer := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "ID\tNAME\tURL\tCOMMAND\tAGENT\tADDITIONS")
	for _, service := range value.Services {
		agent := "yes"
		if service.NoAgent {
			agent = "no"
		}
		fmt.Fprintf(writer, "%d\t%s\thttp://localhost:%d\t%s\t%s\t%s\n", service.ID, service.Name, service.Port, displayCommand(service), agent, service.Additions)
	}
	return writer.Flush()
}

func displayCommand(value service) string {
	parts := append([]string{displayTarget(value)}, value.Args...)
	for i := range parts {
		parts[i] = strconv.Quote(parts[i])
	}
	return strings.Join(parts, " ")
}

func displayTarget(value service) string {
	if value.Target == "." {
		return value.BuildDir
	}
	if filepath.IsAbs(value.Target) {
		return value.Target
	}
	return filepath.Join(value.BuildDir, value.Target)
}
