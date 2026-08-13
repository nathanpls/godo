package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"syscall"
)

type service struct {
	ID        int      `json:"id"`
	Name      string   `json:"name"`
	Kind      string   `json:"kind,omitempty"`
	Target    string   `json:"target"`
	BuildDir  string   `json:"build_dir,omitempty"`
	WorkDir   string   `json:"work_dir"`
	Args      []string `json:"args,omitempty"`
	EnvFile   string   `json:"env_file,omitempty"`
	NoAgent   bool     `json:"no_agent,omitempty"`
	Port      int      `json:"port"`
	Additions string   `json:"additions,omitempty"`
}

type registry struct {
	NextID   int       `json:"next_id"`
	Services []service `json:"services"`
}

func (r *registry) nextID() int {
	if r.NextID < 1 {
		r.NextID = 1
	}
	id := r.NextID
	r.NextID++
	return id
}

func (r *registry) service(id int) (service, bool) {
	for _, candidate := range r.Services {
		if candidate.ID == id {
			return candidate, true
		}
	}
	return service{}, false
}

func (r *registry) remove(id int) {
	r.Services = slices.DeleteFunc(r.Services, func(candidate service) bool {
		return candidate.ID == id
	})
}

type store struct {
	configDir string
	dataDir   string
}

func (s store) load() (registry, error) {
	content, err := os.ReadFile(filepath.Join(s.configDir, "services.json"))
	if errors.Is(err, os.ErrNotExist) {
		return registry{NextID: 1}, nil
	}
	if err != nil {
		return registry{}, fmt.Errorf("read service registry: %w", err)
	}

	var result registry
	if err := json.Unmarshal(content, &result); err != nil {
		return registry{}, fmt.Errorf("decode service registry: %w", err)
	}
	if result.NextID < 1 {
		result.NextID = 1
		for _, service := range result.Services {
			result.NextID = max(result.NextID, service.ID+1)
		}
	}
	for i := range result.Services {
		if result.Services[i].Kind == "" {
			result.Services[i].Kind = "go"
		}
		if result.Services[i].BuildDir == "" {
			result.Services[i].BuildDir = result.Services[i].WorkDir
		}
	}
	slices.SortFunc(result.Services, func(a, b service) int { return a.ID - b.ID })
	return result, nil
}

func (s store) save(value registry) error {
	if err := os.MkdirAll(s.configDir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode service registry: %w", err)
	}
	content = append(content, '\n')
	return writeAtomic(filepath.Join(s.configDir, "services.json"), content, 0o600)
}

func (s store) lock() (func(), error) {
	if err := os.MkdirAll(s.configDir, 0o700); err != nil {
		return nil, fmt.Errorf("create config directory: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(s.configDir, "registry.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open registry lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		file.Close()
		return nil, fmt.Errorf("lock service registry: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

func writeAtomic(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".godo-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)

	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}
