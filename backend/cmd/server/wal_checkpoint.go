package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

// A WAL larger than this is worth merging even if it does not block startup.
const walCheckpointThreshold = 32 << 20

type walCheckpointResult struct {
	Path   string
	Before int64
	After  int64
	Busy   bool
}

// checkpointLargeWALs merges oversized write-ahead logs back into their
// databases. It is best-effort: the backend can serve from the WAL when a
// reader prevents a checkpoint, and a failure here must never block startup or
// shutdown.
func checkpointLargeWALs(ctx context.Context, paths []string, threshold int64, logger *log.Logger) []walCheckpointResult {
	results := make([]walCheckpointResult, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		info, err := os.Stat(path + "-wal")
		if err != nil || (threshold > 0 && info.Size() < threshold) {
			continue
		}
		before := info.Size()
		result, err := checkpointOneWAL(ctx, path)
		if err != nil {
			if logger != nil {
				logger.Printf("level=warn event=wal_checkpoint path=%q error=%q", path, err)
			}
			continue
		}
		result.Path, result.Before = path, before
		if info, statErr := os.Stat(path + "-wal"); statErr == nil {
			result.After = info.Size()
		}
		if logger != nil {
			logger.Printf("level=info event=wal_checkpoint path=%q before=%d after=%d busy=%t", path, result.Before, result.After, result.Busy)
		}
		results = append(results, result)
	}
	return results
}

func checkpointOneWAL(ctx context.Context, path string) (walCheckpointResult, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return walCheckpointResult{}, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := db.ExecContext(callCtx, "PRAGMA busy_timeout=5000"); err != nil {
		return walCheckpointResult{}, err
	}
	result := walCheckpointResult{}
	var busy, logFrames, checkpointed int
	err = db.QueryRowContext(callCtx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed)
	if err != nil || busy != 0 {
		// A reader holds a snapshot: merge what can be merged without blocking
		// it. Keep the original error only when the passive attempt also fails.
		busy = 0
		passiveErr := db.QueryRowContext(callCtx, "PRAGMA wal_checkpoint(PASSIVE)").Scan(&busy, &logFrames, &checkpointed)
		if passiveErr != nil && err != nil {
			return result, err
		}
	}
	result.Busy = busy != 0
	return result, nil
}
