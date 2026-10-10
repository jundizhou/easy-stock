package sqliteutil

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func testDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = db.Ping(); err != nil {
		t.Fatal(err)
	}
	return db
}
func TestWALReaderDoesNotBlockWriterAndSettingsSurviveReconnect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review #1?测试.db")
	writer := testDB(t, path)
	if _, err := writer.Exec(`CREATE TABLE reports (id INTEGER PRIMARY KEY, content TEXT); INSERT INTO reports VALUES(1,'before')`); err != nil {
		t.Fatal(err)
	}
	reader := testDB(t, path)
	tx, err := reader.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var old string
	if err = tx.QueryRow(`SELECT content FROM reports WHERE id=1`).Scan(&old); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err = writer.ExecContext(ctx, `UPDATE reports SET content='after' WHERE id=1`); err != nil {
		t.Fatal("reader blocked write", err)
	}
	var snapshot string
	if err = tx.QueryRow(`SELECT content FROM reports WHERE id=1`).Scan(&snapshot); err != nil || snapshot != "before" {
		t.Fatal("reader snapshot changed", err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var latest string
	if err = reader.QueryRow(`SELECT content FROM reports WHERE id=1`).Scan(&latest); err != nil || latest != "after" {
		t.Fatal("write not persisted", err)
	}
	// Force a replacement connection, as database/sql may do after cancellation.
	reader.SetMaxIdleConns(0)
	for i := 0; i < 2; i++ {
		conn, err := reader.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var mode string
		var wait int
		if err = conn.QueryRowContext(context.Background(), `PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
			t.Fatal("missing WAL", err, mode)
		}
		if err = conn.QueryRowContext(context.Background(), `PRAGMA busy_timeout`).Scan(&wait); err != nil || wait != 5000 {
			t.Fatal("lost busy handler", err, wait)
		}
		conn.Close()
	}
}
func TestMemoryURI(t *testing.T) {
	db := testDB(t, "file:locking-test?mode=memory&cache=shared")
	if _, err := db.Exec(`CREATE TABLE fixture(id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "memory" {
		t.Fatal("memory store became a disk file", err, mode)
	}
}

type codeError int

func (e codeError) Error() string { return "fixture database error" }
func (e codeError) Code() int     { return int(e) }
func TestIsBusyHandlesWrappedSQLiteErrors(t *testing.T) {
	for _, code := range []int{5, 6, 517, 262} {
		if !IsBusy(fmt.Errorf("save schedule: %w", codeError(code))) {
			t.Fatal("missed lock code", code)
		}
	}
	if IsBusy(nil) || IsBusy(fmt.Errorf("database is locked")) || IsBusy(codeError(19)) {
		t.Fatal("non-lock error classified busy")
	}
}
