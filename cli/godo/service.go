package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	workDir, target, defaultName, err := resolveTarget(a.cwd, options.target)
	if err != nil {
		return err
	}
	name := options.name
	if name == "" {
		name = defaultName
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
		Target:    target,
		WorkDir:   workDir,
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
	if err := a.store.save(value); err != nil {
		return err
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

	command := exec.Command("go", "build", "-o", temporary, service.Target)
	command.Dir = service.WorkDir
	command.Stdout = os.Stderr
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return errors.New("go executable not found")
		}
		return fmt.Errorf("build %s: %w", displayTarget(service), err)
	}
	if err := os.Rename(temporary, destination); err != nil {
		return fmt.Errorf("store service binary: %w", err)
	}
	return nil
}
