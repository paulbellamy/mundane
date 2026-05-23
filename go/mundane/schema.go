package mundane

import (
	"database/sql"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
)

const SchemaVersion = "1"

const NamePattern = `^[A-Za-z0-9][A-Za-z0-9._-]*$`

var nameRe = regexp.MustCompile(NamePattern)

func ValidateName(name string) error {
	if !nameRe.MatchString(name) {
		return &InvalidNameError{Name: name}
	}
	return nil
}

const bootstrapDDL = `
CREATE TABLE IF NOT EXISTS mundane_meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS mundane_steps (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  name         TEXT NOT NULL,
  kind         TEXT NOT NULL,
  encoding     TEXT NOT NULL,
  result       BLOB,
  status       TEXT NOT NULL,
  error        TEXT,
  started_at   TEXT NOT NULL,
  finished_at  TEXT,
  UNIQUE(name)
);

CREATE INDEX IF NOT EXISTS mundane_steps_status ON mundane_steps(status);
`

// Bootstrap creates the schema if missing and seeds task_id / schema_version /
// created_at. Idempotent. Returns SchemaError if an existing schema_version
// row disagrees with the pinned version.
func Bootstrap(db *sql.DB, path string) error {
	if _, err := db.Exec("PRAGMA journal_mode = DELETE"); err != nil {
		return fmt.Errorf("set journal_mode: %w", err)
	}

	// Pre-check: existing mismatched schema_version is a hard error before we
	// run any CREATE INDEX (which references v1 columns).
	var existing string
	err := db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='mundane_meta'",
	).Scan(&existing)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("check meta table: %w", err)
	}
	if existing != "" {
		var ver string
		err := db.QueryRow(
			"SELECT value FROM mundane_meta WHERE key='schema_version'",
		).Scan(&ver)
		if err == nil && ver != SchemaVersion {
			return &SchemaError{Path: path, Version: ver}
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(bootstrapDDL); err != nil {
		return fmt.Errorf("bootstrap DDL: %w", err)
	}

	now := IsoNow()
	taskID := uuid.New().String()
	if _, err := tx.Exec(
		"INSERT OR IGNORE INTO mundane_meta (key, value) VALUES ('schema_version', ?)",
		SchemaVersion,
	); err != nil {
		return fmt.Errorf("seed schema_version: %w", err)
	}
	if _, err := tx.Exec(
		"INSERT OR IGNORE INTO mundane_meta (key, value) VALUES ('task_id', ?)",
		taskID,
	); err != nil {
		return fmt.Errorf("seed task_id: %w", err)
	}
	if _, err := tx.Exec(
		"INSERT OR IGNORE INTO mundane_meta (key, value) VALUES ('created_at', ?)",
		now,
	); err != nil {
		return fmt.Errorf("seed created_at: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	// Post-bootstrap re-check.
	var ver string
	if err := db.QueryRow(
		"SELECT value FROM mundane_meta WHERE key='schema_version'",
	).Scan(&ver); err != nil {
		return fmt.Errorf("re-check schema_version: %w", err)
	}
	if ver != SchemaVersion {
		return &SchemaError{Path: path, Version: ver}
	}
	return nil
}

func IsoNow() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

func NowMs() int64 {
	return time.Now().UnixMilli()
}
