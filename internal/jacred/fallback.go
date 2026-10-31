package jacred

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

type fallback struct {
	primary, backup Searcher
	primaryTimeout  time.Duration
}

// NewFallback tries backup after a primary error, within the caller's context.
// primaryTimeout reserves time for backup when the primary does not respond.
func NewFallback(primary, backup Searcher, primaryTimeout time.Duration) Searcher {
	return fallback{primary: primary, backup: backup, primaryTimeout: primaryTimeout}
}

func (f fallback) Search(ctx context.Context, q Query) ([]Result, error) {
	primaryCtx, cancel := context.WithTimeout(ctx, f.primaryTimeout)
	results, primaryErr := f.primary.Search(primaryCtx, q)
	cancel()
	if primaryErr == nil {
		return results, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	results, err := f.backup.Search(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("primary JacRed: %w; fallback JacRed: %w", primaryErr, err)
	}
	slog.WarnContext(ctx, "JacRed fallback served search", "primary_error", primaryErr, "results", len(results))
	return results, nil
}
