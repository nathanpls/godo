package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (a *app) add(options addOptions) error {
	unlock, err := a.store.lock()
	if err != nil {
		return err
	}
	defer unlock()

	value, err := a.store.load()
	if err != nil {
		return err
	}
	resolved, err := resolveTarget(a.cwd, options.target)
	if err != nil {
		return err
	}
	workDir := resolved.workDir
	if options.workDir != "" {
		workDir, err = resolveWorkDir(a.cwd, options.workDir)
		if err != nil {
			return err
		}
	}
	envFile, err := resolveEnvFile(a.cwd, options.envFile)
	if err != nil {
		return err
	}
	name := options.name
	if name == "" {
		name = resolved.defaultName
	}
	for _, existing := range value.Services {
		if existing.Name == name {
			return fmt.Errorf("service name %q is already in use", name)
		}
	}
	port, err := choosePort(options.port, value.Services)
	if err != nil {
		return err
	}

	candidate := service{
		ID:        value.NextID,
		Name:      name,
		Kind:      resolved.kind,
		Target:    resolved.target,
		BuildDir:  resolved.buildDir,
		WorkDir:   workDir,
		Args:      options.args,
		EnvFile:   envFile,
		NoAgent:   options.noAgent,
		Port:      port,
		Additions: options.additions,
	}
	if candidate.ID < 1 {
		candidate.ID = 1
	}
	binary := a.binaryPath(candidate.ID)
	if err := buildService(candidate, binary); err != nil {
		return err
	}
	if err := a.supervisor.install(candidate, binary); err != nil {
		_ = os.RemoveAll(a.serviceDir(candidate.ID))
		return err
	}

	value.nextID()
	value.Services = append(value.Services, candidate)
	if err := a.store.save(value); err != nil {
		_ = a.supervisor.remove(candidate)
		_ = os.RemoveAll(a.serviceDir(candidate.ID))
		return err
	}
	if err := syncAgents(a.agentsFile, value.Services); err != nil {
		return fmt.Errorf("service started, but agent discovery failed: %w", err)
	}

	fmt.Fprintf(a.stdout, "Added service %d: %s at http://localhost:%d\n", candidate.ID, candidate.Name, candidate.Port)
	return nil
}

func (a *app) update(id int) error {
	unlock, err := a.store.lock()
	if err != nil {
		return err
	}
	defer unlock()

	value, err := a.store.load()
	if err != nil {
		return err
	}
	service, found := value.service(id)
	if !found {
		return fmt.Errorf("service %d does not exist", id)
	}
	if err := validateServiceRuntime(service); err != nil {
		return err
	}

	binary := a.binaryPath(id)
	next := binary + ".next"
	previous := binary + ".previous"
	if err := buildService(service, next); err != nil {
		return err
	}
	defer os.Remove(next)
	_ = os.Remove(previous)
	if err := os.Rename(binary, previous); err != nil {
		return fmt.Errorf("prepare service update: %w", err)
	}
	if err := os.Rename(next, binary); err != nil {
		_ = os.Rename(previous, binary)
		return fmt.Errorf("activate service update: %w", err)
	}
	if err := a.supervisor.restart(service); err != nil {
		_ = os.Remove(binary)
		if restoreErr := os.Rename(previous, binary); restoreErr != nil {
			return fmt.Errorf("restart updated service: %v; restore previous binary: %w", err, restoreErr)
		}
		if restoreErr := a.supervisor.restart(service); restoreErr != nil {
			return fmt.Errorf("restart updated service: %v; restart restored service: %w", err, restoreErr)
		}
		return fmt.Errorf("updated service failed to start; restored previous version: %w", err)
	}
	_ = os.Remove(previous)

	fmt.Fprintf(a.stdout, "Updated service %d: %s\n", service.ID, service.Name)
	return nil
}

func (a *app) restart(id int) error {
	unlock, err := a.store.lock()
	if err != nil {
		return err
	}
	defer unlock()
	value, err := a.store.load()
	if err != nil {
		return err
	}
	service, found := value.service(id)
	if !found {
		return fmt.Errorf("service %d does not exist", id)
	}
	if err := validateServiceRuntime(service); err != nil {
		return err
	}
	if err := a.supervisor.restart(service); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Restarted service %d: %s\n", service.ID, service.Name)
	return nil
}

func (a *app) remove(id int) error {
	unlock, err := a.store.lock()
	if err != nil {
		return err
	}
	defer unlock()

	value, err := a.store.load()
	if err != nil {
		return err
	}
	service, found := value.service(id)
	if !found {
		return fmt.Errorf("service %d does not exist", id)
	}
	if err := a.supervisor.remove(service); err != nil {
		return err
	}
	value.remove(id)
	if err := a.store.save(value); err != nil {
		return err
	}
	if err := os.RemoveAll(a.serviceDir(id)); err != nil {
		return fmt.Errorf("remove service files: %w", err)
	}
	if err := syncAgents(a.agentsFile, value.Services); err != nil {
		return fmt.Errorf("service removed, but agent discovery failed: %w", err)
	}

	fmt.Fprintf(a.stdout, "Removed service %d: %s\n", service.ID, service.Name)
	return nil
}

func (a *app) edit(options serviceEditOptions) error {
	unlock, err := a.store.lock()
	if err != nil {
		return err
	}
	defer unlock()

	value, err := a.store.load()
	if err != nil {
		return err
	}
	index := -1
	for i, candidate := range value.Services {
		if candidate.ID == options.id {
			index = i
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("service %d does not exist", options.id)
	}
	previous := value.Services[index]
	if options.nameSet {
		for _, candidate := range value.Services {
			if candidate.ID != options.id && candidate.Name == options.name {
				return fmt.Errorf("service name %q is already in use", options.name)
			}
		}
		value.Services[index].Name = options.name
	}
	if options.additionsSet {
		value.Services[index].Additions = options.additions
	}
	runtimeChanged := options.workDirSet || options.envFileSet || options.argsSet
	if options.workDirSet {
		workDir, err := resolveWorkDir(a.cwd, options.workDir)
		if err != nil {
			return err
		}
		value.Services[index].WorkDir = workDir
	}
	if options.envFileSet {
		envFile, err := resolveEnvFile(a.cwd, options.envFile)
		if err != nil {
			return err
		}
		value.Services[index].EnvFile = envFile
	}
	if options.argsSet {
		value.Services[index].Args = options.args
	}
	if options.agentSet {
		value.Services[index].NoAgent = options.noAgent
	}
	if runtimeChanged {
		if err := validateServiceRuntime(value.Services[index]); err != nil {
			return err
		}
	}
	if err := a.store.save(value); err != nil {
		return err
	}
	if runtimeChanged {
		if err := a.supervisor.configure(value.Services[index], a.binaryPath(options.id)); err != nil {
			restored := value
			restored.Services = append([]service(nil), value.Services...)
			restored.Services[index] = previous
			if restoreErr := a.store.save(restored); restoreErr != nil {
				return fmt.Errorf("configure service: %v; restore registry: %w", err, restoreErr)
			}
			return err
		}
	}
	if err := syncAgents(a.agentsFile, value.Services); err != nil {
		return fmt.Errorf("service metadata updated, but agent discovery failed: %w", err)
	}
	fmt.Fprintf(a.stdout, "Edited service %d: %s\n", value.Services[index].ID, value.Services[index].Name)
	return nil
}

func (a *app) syncAgentsAndPrint() error {
	unlock, err := a.store.lock()
	if err != nil {
		return err
	}
	defer unlock()
	value, err := a.store.load()
	if err != nil {
		return err
	}
	if err := syncAgents(a.agentsFile, value.Services); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Updated %s\n", a.agentsFile)
	return nil
}

func (a *app) serviceDir(id int) string {
	return filepath.Join(a.store.dataDir, "services", fmt.Sprintf("%d", id))
}

func (a *app) binaryPath(id int) string {
	return filepath.Join(a.serviceDir(id), "service")
}

func buildService(service service, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create service directory: %w", err)
	}
	temporary := destination + ".build"
	_ = os.Remove(temporary)
	defer os.Remove(temporary)

	if service.Kind == "executable" {
		if err := copyExecutable(service.Target, temporary); err != nil {
			return err
		}
	} else {
		command := exec.Command("go", "build", "-o", temporary, service.Target)
		command.Dir = service.BuildDir
		command.Stdout = os.Stderr
		command.Stderr = os.Stderr
		if err := command.Run(); err != nil {
			if errors.Is(err, exec.ErrNotFound) {
				return errors.New("go executable not found")
			}
			return fmt.Errorf("build %s: %w", displayTarget(service), err)
		}
	}
	if err := os.Rename(temporary, destination); err != nil {
		return fmt.Errorf("store service binary: %w", err)
	}
	return nil
}

func copyExecutable(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("inspect service executable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return errors.New("service executable must be a regular executable file")
	}
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open service executable: %w", err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("create managed executable: %w", err)
	}
	success := false
	defer func() {
		_ = output.Close()
		if !success {
			_ = os.Remove(destination)
		}
	}()
	if _, err := io.Copy(output, input); err != nil {
		return fmt.Errorf("copy service executable: %w", err)
	}
	if err := output.Sync(); err != nil {
		return fmt.Errorf("sync service executable: %w", err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close service executable: %w", err)
	}
	success = true
	return nil
}

func resolveWorkDir(cwd, value string) (string, error) {
	if value == "" {
		return "", errors.New("service working directory must not be empty")
	}
	resolved, err := resolveUserPath(cwd, value)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("service working directory %q is not a directory", resolved)
	}
	return resolved, nil
}

func resolveEnvFile(cwd, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	resolved, err := resolveUserPath(cwd, value)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect service environment file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("service environment file must be a regular non-symlink file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("service environment file must not be accessible by group or others")
	}
	file, err := os.Open(resolved)
	if err != nil {
		return "", fmt.Errorf("open service environment file: %w", err)
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.HasPrefix(strings.TrimSpace(scanner.Text()), "PORT=") {
			file.Close()
			return "", errors.New("service environment file must not define reserved PORT")
		}
	}
	if err := scanner.Err(); err != nil {
		file.Close()
		return "", errors.New("read service environment file")
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close service environment file: %w", err)
	}
	return resolved, nil
}

func resolveUserPath(cwd, value string) (string, error) {
	if strings.ContainsRune(value, 0) || strings.ContainsAny(value, "\r\n") {
		return "", errors.New("service paths must not contain NUL bytes or newlines")
	}
	if value == "~" || len(value) > 1 && value[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		value = filepath.Join(home, strings.TrimPrefix(value, "~/"))
	} else if strings.HasPrefix(value, "~") {
		return "", errors.New("only ~/ home-relative paths are supported")
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(cwd, value)
	}
	return filepath.Abs(value)
}

func validateServiceRuntime(service service) error {
	if _, err := resolveWorkDir(service.WorkDir, service.WorkDir); err != nil {
		return err
	}
	if service.EnvFile != "" {
		if _, err := resolveEnvFile(service.WorkDir, service.EnvFile); err != nil {
			return err
		}
	}
	for _, argument := range service.Args {
		if strings.ContainsRune(argument, 0) || strings.ContainsAny(argument, "\r\n") {
			return errors.New("service arguments must not contain NUL bytes or newlines")
		}
	}
	return nil
}
