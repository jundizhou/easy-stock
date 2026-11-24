package main

import (
	"context"
	"database/sql"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestCheckpointLargeWALsKeepsDataAndShrinksWhenPossible(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA wal_autocheckpoint=0",
		"CREATE TABLE sample (value TEXT NOT NULL)",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(statement, err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	payload := strings.Repeat("x", 4096)
	for i := 0; i < 2000; i++ {
		if _, err := tx.Exec("INSERT INTO sample (value) VALUES (?)", payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path + "-wal")
	if err != nil || info.Size() == 0 {
		t.Fatalf("test did not create a WAL: %v", err)
	}
	before := info.Size()

	results := checkpointLargeWALs(context.Background(), []string{path}, 0, log.New(io.Discard, "", 0))
	if len(results) != 1 {
		t.Fatalf("checkpoint results = %v", results)
	}
	if results[0].Before != before || results[0].Path != path {
		t.Fatalf("checkpoint result = %+v, before=%d", results[0], before)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sample").Scan(&count); err != nil || count != 2000 {
		t.Fatalf("data after checkpoint count=%d err=%v", count, err)
	}
	if !results[0].Busy && results[0].After > results[0].Before {
		t.Fatalf("checkpoint grew the WAL: %+v", results[0])
	}
}

func TestCheckpointLargeWALsSkipsSmallOrMissingFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE sample (value TEXT)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if results := checkpointLargeWALs(context.Background(), []string{path, filepath.Join(dir, "missing.db")}, 1<<30, log.New(io.Discard, "", 0)); len(results) != 0 {
		t.Fatalf("small WAL was checkpointed: %v", results)
	}
}
