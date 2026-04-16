package config

import (
	"fmt"
	"log/slog"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration for contextd.
type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Storage StorageConfig `yaml:"storage"`
	Search  SearchConfig  `yaml:"search"`
	Policy  PolicyConfig  `yaml:"policy"`
	Audit   AuditConfig   `yaml:"audit"`
}

// ServerConfig controls the HTTP listener.
type ServerConfig struct {
	Host string `yaml:"host"` // default "127.0.0.1"
	Port int    `yaml:"port"` // default 8080
}

// StorageConfig controls the persistence backend.
type StorageConfig struct {
	Type        string `yaml:"type"`        // "sqlite" only for v0.1
	Path        string `yaml:"path"`        // default "./data/contextd.db"
	Compression bool   `yaml:"compression"` // default false
}

// SearchConfig controls full-text and vector search behaviour.
type SearchConfig struct {
	FullText    bool    `yaml:"full_text"`    // default true
	Vector      bool    `yaml:"vector"`       // default false
	HybridAlpha float64 `yaml:"hybrid_alpha"` // BM25 weight, default 0.5
	HybridBeta  float64 `yaml:"hybrid_beta"`  // vector weight, default 0.4
	HybridGamma float64 `yaml:"hybrid_gamma"` // temporal weight, default 0.1
}

// PolicyConfig controls data-access policies.
type PolicyConfig struct {
	DefaultRetentionDays int `yaml:"default_retention_days"` // default 90
	MaxResultsPerQuery   int `yaml:"max_results_per_query"`  // default 100
}

// AuditConfig controls the audit log behaviour.
type AuditConfig struct {
	Enabled       bool `yaml:"enabled"`        // default true
	RetentionDays int  `yaml:"retention_days"` // default 365
}

// Default returns a Config populated with sensible defaults.
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Host: "127.0.0.1",
			Port: 8080,
		},
		Storage: StorageConfig{
			Type:        "sqlite",
			Path:        "./data/contextd.db",
			Compression: false,
		},
		Search: SearchConfig{
			FullText:    true,
			Vector:      false,
			HybridAlpha: 0.5,
			HybridBeta:  0.4,
			HybridGamma: 0.1,
		},
		Policy: PolicyConfig{
			DefaultRetentionDays: 90,
			MaxResultsPerQuery:   100,
		},
		Audit: AuditConfig{
			Enabled:       true,
			RetentionDays: 365,
		},
	}
}

// Load reads the YAML config file at path, falling back to defaults for any
// missing fields. If the file is absent, defaults are returned with a warning.
func Load(path string) (*Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Warn("config file not found, using defaults", "path", path)
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config file %q: %w", path, err)
	}

	// Unmarshal into a separate struct; any missing fields retain their defaults.
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %q: %w", path, err)
	}

	warnHybridWeights(cfg)
	return cfg, nil
}

// warnHybridWeights logs a warning if the hybrid weights do not sum to ~1.0.
func warnHybridWeights(cfg *Config) {
	sum := cfg.Search.HybridAlpha + cfg.Search.HybridBeta + cfg.Search.HybridGamma
	const tolerance = 0.01
	if sum < 1.0-tolerance || sum > 1.0+tolerance {
		slog.Warn(
			"hybrid search weights do not sum to 1.0",
			"alpha", cfg.Search.HybridAlpha,
			"beta", cfg.Search.HybridBeta,
			"gamma", cfg.Search.HybridGamma,
			"sum", sum,
		)
	}
}

// Addr returns the host:port string for the HTTP listener.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}
