package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryIDsAreNotReused(t *testing.T) {
	value := registry{NextID: 1}
	first := value.nextID()
	second := value.nextID()
	value.Services = []service{{ID: second}}
	value.remove(first)

	if next := value.nextID(); next != 3 {
		t.Fatalf("next ID = %d, want 3", next)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	directory := t.TempDir()
	storage := store{configDir: filepath.Join(directory, "config"), dataDir: filepath.Join(directory, "data")}
	want := registry{NextID: 2, Services: []service{{ID: 1, Name: "docs", Port: 41000}}}

	if err := storage.save(want); err != nil {
		t.Fatal(err)
	}
	got, err := storage.load()
	if err != nil {
		t.Fatal(err)
	}
	if got.NextID != want.NextID || len(got.Services) != 1 || got.Services[0].Name != "docs" {
		t.Fatalf("registry = %+v, want %+v", got, want)
	}
}

func TestStoreLoadsLegacyServiceDefaults(t *testing.T) {
	directory := t.TempDir()
	storage := store{configDir: directory}
	content := `{"next_id":2,"services":[{"id":1,"name":"docs","target":".","work_dir":"/tmp/docs","port":41000}]}`
	if err := os.WriteFile(filepath.Join(directory, "services.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := storage.load()
	if err != nil {
		t.Fatal(err)
	}
	if value.Services[0].Kind != "go" || value.Services[0].BuildDir != "/tmp/docs" {
		t.Fatalf("legacy service = %+v", value.Services[0])
	}
}
