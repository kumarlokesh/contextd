// Package privacy implements GDPR-oriented helpers: retention enforcement and
// project data export.
package privacy

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kumarlokesh/contextd/audit"
	"github.com/kumarlokesh/contextd/store"
)

// Enforcer periodically sweeps old chats according to per-project or global
// retention policy and logs a deletion audit entry when chats are removed.
type Enforcer struct {
	st            store.Store
	auditor       audit.Logger // nil = no audit entries written
	defaultDays   int
	sweepInterval time.Duration
	logger        *slog.Logger
}

// NewEnforcer creates an Enforcer. Pass nil for al to skip audit logging.
func NewEnforcer(st store.Store, al audit.Logger, defaultDays int, sweepInterval time.Duration, logger *slog.Logger) *Enforcer {
	return &Enforcer{
		st:            st,
		auditor:       al,
		defaultDays:   defaultDays,
		sweepInterval: sweepInterval,
		logger:        logger,
	}
}

// Start runs retention sweeps on the configured interval until ctx is cancelled.
// It performs an initial sweep immediately on entry.
func (e *Enforcer) Start(ctx context.Context) {
	e.runSweep(ctx)
	ticker := time.NewTicker(e.sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			e.runSweep(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (e *Enforcer) runSweep(ctx context.Context) {
	n, err := e.Sweep(ctx)
	if err != nil {
		e.logger.Error("retention sweep failed", "err", err)
		return
	}
	if n > 0 {
		e.logger.Info("retention sweep complete", "chats_deleted", n)
	}
}

// Sweep runs one retention pass over all projects and returns the total number
// of chats deleted. Per-project overrides take precedence over the default.
func (e *Enforcer) Sweep(ctx context.Context) (int, error) {
	projects, err := e.st.AllProjectIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("retention sweep: listing projects: %w", err)
	}

	now := time.Now().UTC()
	total := 0
	for _, projectID := range projects {
		days := e.defaultDays
		if override, err := e.st.ProjectRetention(ctx, projectID); err == nil && override > 0 {
			days = override
		}
		cutoff := now.AddDate(0, 0, -days)

		n, err := e.st.DeleteChatsOlderThan(ctx, projectID, cutoff)
		if err != nil {
			e.logger.Error("retention sweep: delete failed",
				"project", projectID, "err", err)
			continue
		}
		if n == 0 {
			continue
		}
		total += n
		if e.auditor != nil {
			_ = e.auditor.Log(ctx, audit.Entry{
				ProjectID: projectID,
				Action:    audit.ActionDelete,
				Metadata: map[string]any{
					"reason":         "retention",
					"retention_days": days,
					"chats_deleted":  n,
					"cutoff":         cutoff.Format(time.RFC3339),
				},
			})
		}
	}
	return total, nil
}
