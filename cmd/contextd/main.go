package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/kumarlokesh/contextd/api"
	"github.com/kumarlokesh/contextd/audit"
	"github.com/kumarlokesh/contextd/config"
	"github.com/kumarlokesh/contextd/embed"
	"github.com/kumarlokesh/contextd/privacy"
	"github.com/kumarlokesh/contextd/search"
	"github.com/kumarlokesh/contextd/server"
	sqlitestore "github.com/kumarlokesh/contextd/store/sqlite"
)

// Injected at build time via -ldflags.
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "contextd: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	switch args[0] {
	case "serve":
		return cmdServe(args[1:])
	case "version":
		return cmdVersion(args[1:])
	case "init-config":
		return cmdInitConfig(args[1:])
	case "verify-audit":
		return cmdVerifyAudit(args[1:])
	case "-h", "--help", "help":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// cmdServe starts the HTTP daemon.
func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := fs.String("config", "contextd.yaml", "path to config file")
	port := fs.Int("port", 0, "override listen port (0 = use config)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: contextd serve [flags]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	logger := buildLogger()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if *port != 0 {
		cfg.Server.Port = *port
	}

	// Ensure the data directory exists before opening SQLite.
	if err := os.MkdirAll(filepath.Dir(cfg.Storage.Path), 0o755); err != nil {
		return fmt.Errorf("creating data dir: %w", err)
	}

	st, err := sqlitestore.Open(cfg.Storage.Path)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer st.Close()

	// FTS5 searcher shares the same SQLite connection as the store.
	var sr search.Searcher
	var startWorker func(context.Context) // set when vector search is enabled
	if cfg.Search.FullText {
		fts, err := search.NewFTSSearcher(st.DB())
		if err != nil {
			return fmt.Errorf("initialising FTS searcher: %w", err)
		}
		defer fts.Close()
		sr = fts
		logger.Info("FTS5 full-text search enabled")

		// Vector search + hybrid ranking (requires vector=true and an embed type).
		if cfg.Search.Vector && cfg.Embed.Type != "" {
			vs, err := sqlitestore.NewVecStore(st.DB())
			if err != nil {
				return fmt.Errorf("initialising vec store: %w", err)
			}
			defer vs.Close()

			embedder, err := buildEmbedder(cfg.Embed)
			if err != nil {
				return fmt.Errorf("initialising embedder: %w", err)
			}
			defer embedder.Close()

			pollInterval, err := time.ParseDuration(cfg.Embed.PollInterval)
			if err != nil {
				pollInterval = 5 * time.Second
			}
			w := embed.NewWorker(vs, embedder, cfg.Embed.BatchSize, pollInterval, logger)
			// startWorker is called after ctx is created below.
			startWorker = func(c context.Context) { go w.Run(c) }

			sr = search.NewHybridSearcher(fts, vs, embedder,
				cfg.Search.HybridAlpha, cfg.Search.HybridBeta, cfg.Search.HybridGamma)
			logger.Info("hybrid search enabled",
				"embed_type", cfg.Embed.Type,
				"alpha", cfg.Search.HybridAlpha,
				"beta", cfg.Search.HybridBeta,
				"gamma", cfg.Search.HybridGamma,
			)
		}
	}

	// Audit logger shares the same SQLite connection as the store.
	var al audit.Logger
	if cfg.Audit.Enabled {
		aLogger, err := audit.NewSQLiteLogger(st.DB())
		if err != nil {
			return fmt.Errorf("initialising audit logger: %w", err)
		}
		defer aLogger.Close()
		al = aLogger
		logger.Info("audit log enabled")
	}

	build := server.BuildInfo{
		Version:   version,
		Commit:    commit,
		BuildDate: buildDate,
	}

	// Retention enforcer: sweeps old chats according to per-project or default policy.
	sweepInterval, err := time.ParseDuration(cfg.Policy.RetentionSweepInterval)
	if err != nil {
		sweepInterval = 24 * time.Hour
	}
	enforcer := privacy.NewEnforcer(st, al, cfg.Policy.DefaultRetentionDays, sweepInterval, logger)

	srv := server.New(cfg, logger, build)
	srv.Routes()
	srv.MountAPI("/v1", api.Router(st, sr, al, cfg.Policy.MaxResultsPerQuery, cfg.Policy.DefaultRetentionDays))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if startWorker != nil {
		startWorker(ctx)
	}
	go enforcer.Start(ctx)

	return srv.Start(ctx)
}

// cmdVersion prints build information and exits.
func cmdVersion(args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: contextd version")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	fmt.Printf("version:    %s\ncommit:     %s\nbuild_date: %s\n", version, commit, buildDate)
	return nil
}

// cmdInitConfig writes a default contextd.yaml to disk.
func cmdInitConfig(args []string) error {
	fs := flag.NewFlagSet("init-config", flag.ContinueOnError)
	out := fs.String("output", "contextd.yaml", "path to write the default config")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: contextd init-config [flags]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if _, err := os.Stat(*out); err == nil {
		return fmt.Errorf("file %q already exists; remove it first", *out)
	}

	data, err := yaml.Marshal(config.Default())
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}

	if err := os.WriteFile(*out, data, 0o644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	fmt.Printf("wrote default config to %s\n", *out)
	return nil
}

// buildLogger constructs a slog.Logger that respects the CONTEXTD_LOG_LEVEL
// environment variable.
func buildLogger() *slog.Logger {
	level := slog.LevelInfo
	if v := os.Getenv("CONTEXTD_LOG_LEVEL"); v != "" {
		var l slog.Level
		if err := l.UnmarshalText([]byte(v)); err == nil {
			level = l
		}
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}

// buildEmbedder constructs an Embedder based on the config type.
// Returns an error if the type is unrecognised or required config is absent.
func buildEmbedder(cfg config.EmbedConfig) (embed.Embedder, error) {
	switch cfg.Type {
	case "ollama":
		return embed.NewOllamaEmbedder(cfg.OllamaURL, cfg.OllamaModel), nil
	case "openai":
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY env var is required for openai embedder")
		}
		return embed.NewOpenAIEmbedder(key, cfg.OpenAIModel, cfg.Dimensions), nil
	default:
		return nil, fmt.Errorf("unknown embed type %q (supported: ollama, openai)", cfg.Type)
	}
}

// cmdVerifyAudit opens the audit log at the given database path and verifies
// the hash chain. Exits 0 if valid, 1 if invalid or on error.
func cmdVerifyAudit(args []string) error {
	fs := flag.NewFlagSet("verify-audit", flag.ContinueOnError)
	dbPath := fs.String("db", "./data/contextd.db", "path to the contextd SQLite database")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: contextd verify-audit [--db <path>]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	db, err := openAuditDB(*dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	al, err := audit.NewSQLiteLogger(db)
	if err != nil {
		return fmt.Errorf("initialising audit logger: %w", err)
	}
	defer al.Close()

	result, err := audit.Verify(context.Background(), al)
	if err != nil {
		return fmt.Errorf("verification error: %w", err)
	}

	if result.Valid {
		fmt.Printf("audit chain valid — %d entries checked\n", result.EntriesChecked)
		return nil
	}
	fmt.Fprintf(os.Stderr, "audit chain INVALID: %s (first invalid entry id=%d, checked %d)\n",
		result.Reason, result.FirstInvalidID, result.EntriesChecked)
	os.Exit(1)
	return nil
}

// openAuditDB opens a read-only SQLite connection for audit verification.
func openAuditDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage: contextd <command> [flags]

Commands:
  serve         Start the contextd HTTP daemon
  version       Print version information
  init-config   Write a default contextd.yaml to disk
  verify-audit  Verify the integrity of the audit log hash chain

Run "contextd <command> --help" for command-specific flags.`)
}
