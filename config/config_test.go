package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("expected host 127.0.0.1, got %q", cfg.Server.Host)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Server.Port)
	}
	if cfg.Storage.Type != "sqlite" {
		t.Errorf("expected storage type sqlite, got %q", cfg.Storage.Type)
	}
	if !cfg.Search.FullText {
		t.Error("expected full_text search enabled by default")
	}
	if cfg.Policy.DefaultRetentionDays != 90 {
		t.Errorf("expected default_retention_days 90, got %d", cfg.Policy.DefaultRetentionDays)
	}
	if !cfg.Audit.Enabled {
		t.Error("expected audit enabled by default")
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := Load("/nonexistent/path/contextd.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	// Should return defaults.
	if cfg.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Server.Port)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "bad-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(": invalid: yaml: :[")
	f.Close()

	_, err = Load(f.Name())
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoadPartialConfig(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "partial.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Overridden field.
	if cfg.Server.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Server.Port)
	}
	// Default fields preserved.
	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("expected default host 127.0.0.1, got %q", cfg.Server.Host)
	}
	if !cfg.Audit.Enabled {
		t.Error("expected audit enabled (default)")
	}
}

func TestLoadFullConfig(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "full.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("expected host 0.0.0.0, got %q", cfg.Server.Host)
	}
	if cfg.Server.Port != 7070 {
		t.Errorf("expected port 7070, got %d", cfg.Server.Port)
	}
	if cfg.Storage.Path != "/tmp/test.db" {
		t.Errorf("expected path /tmp/test.db, got %q", cfg.Storage.Path)
	}
	if !cfg.Storage.Compression {
		t.Error("expected compression true")
	}
	if cfg.Policy.MaxResultsPerQuery != 50 {
		t.Errorf("expected max_results 50, got %d", cfg.Policy.MaxResultsPerQuery)
	}
	if cfg.Audit.Enabled {
		t.Error("expected audit disabled")
	}
}

func TestLoadEmptyFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "empty-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	cfg, err := Load(f.Name())
	if err != nil {
		t.Fatalf("unexpected error for empty file: %v", err)
	}
	// Empty file should produce defaults.
	if cfg.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Server.Port)
	}
}

func TestAddr(t *testing.T) {
	cfg := Default()
	if cfg.Addr() != "127.0.0.1:8080" {
		t.Errorf("unexpected addr %q", cfg.Addr())
	}
}
