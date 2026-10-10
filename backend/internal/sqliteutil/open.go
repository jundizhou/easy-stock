// Package sqliteutil configures connection-local SQLite locking behavior.
package sqliteutil

import (
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Open accepts a filesystem path or an existing file: URI. Configure pragmas
// through the DSN so replacement connections retain the busy handler too.
func Open(dataSource string) (*sql.DB, error) {
	var uri *url.URL
	var err error
	if strings.HasPrefix(dataSource, "file:") {
		uri, err = url.Parse(dataSource)
	} else {
		var absolute string
		absolute, err = filepath.Abs(dataSource)
		uriPath := filepath.ToSlash(absolute)
		if !strings.HasPrefix(uriPath, "/") {
			uriPath = "/" + uriPath
		}
		uri = &url.URL{Scheme: "file", Path: uriPath}
	}
	if err != nil {
		return nil, err
	}
	query := uri.Query()
	query.Add("_pragma", "busy_timeout(5000)")
	// WAL lets report readers and schedule writers coexist; writers still wait
	// at most five seconds for one another. In-memory stores do not support WAL.
	if query.Get("mode") != "memory" {
		query.Add("_pragma", "journal_mode(WAL)")
	}
	uri.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}

func IsBusy(err error) bool {
	var sqliteError interface{ Code() int }
	if !errors.As(err, &sqliteError) {
		return false
	}
	code := sqliteError.Code() & 0xff
	return code == 5 || code == 6 // SQLITE_BUSY / SQLITE_LOCKED, including extended codes.
}
