package themelibrary

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenWithSeedDoesNotDowngradeNewerRefresh(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "theme-library.db")

	older := testSeed()
	older.GeneratedAt = "2026-10-19T10:00:00Z"
	newer := testSeed()
	newer.GeneratedAt = "2026-10-20T12:00:00Z"

	store, err := OpenWithSeed(path, older)
	if err != nil {
		t.Fatalf("open with older seed: %v", err)
	}
	if err := store.Replace(ctx, newer); err != nil {
		t.Fatalf("replace with runtime refresh: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := OpenWithSeed(path, older)
	if err != nil {
		t.Fatalf("reopen with older embedded seed: %v", err)
	}
	defer reopened.Close()
	meta, err := reopened.Meta(ctx)
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	if meta.GeneratedAt != newer.GeneratedAt {
		t.Fatalf("generated_at = %q, want runtime refresh %q", meta.GeneratedAt, newer.GeneratedAt)
	}
}
