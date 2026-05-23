package main

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/paulbellamy/mundane/go/mundane"
	_ "modernc.org/sqlite"
)

// Inspection is CLI-only: status / steps / get. The SDKs don't wrap it —
// the binary is the single inspection surface across all runtimes.

func openRO(path string) (*sql.DB, error) {
	return sql.Open("sqlite", "file:"+path+"?mode=ro")
}

func checkSchemaRO(db *sql.DB, path string) error {
	var v string
	if err := db.QueryRow(
		"SELECT value FROM mundane_meta WHERE key='schema_version'",
	).Scan(&v); err != nil {
		return fmt.Errorf("schema_version not found in %s: %w", path, err)
	}
	if v != mundane.SchemaVersion {
		return &mundane.SchemaError{Path: path, Version: v}
	}
	return nil
}

func cmdStatus(args []string) int {
	if len(args) != 1 {
		return die(2, "usage: mundane status <task.db>")
	}
	db, err := openRO(args[0])
	if err != nil {
		return die(1, "%v", err)
	}
	defer db.Close()
	if err := checkSchemaRO(db, args[0]); err != nil {
		return mapErr(err)
	}

	var taskID string
	_ = db.QueryRow("SELECT value FROM mundane_meta WHERE key='task_id'").Scan(&taskID)

	var total, done, pending, failed int
	rows, err := db.Query("SELECT status, COUNT(*) FROM mundane_steps GROUP BY status")
	if err != nil {
		return die(1, "%v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		var c int
		if err := rows.Scan(&s, &c); err != nil {
			return die(1, "%v", err)
		}
		total += c
		switch s {
		case mundane.StatusDone:
			done = c
		case mundane.StatusPending:
			pending = c
		case mundane.StatusFailed:
			failed = c
		}
	}
	fmt.Printf("task_id=%s  steps=%d  done=%d  pending=%d  failed=%d\n",
		taskID, total, done, pending, failed)
	return 0
}

func cmdSteps(args []string) int {
	if len(args) != 1 {
		return die(2, "usage: mundane steps <task.db>")
	}
	db, err := openRO(args[0])
	if err != nil {
		return die(1, "%v", err)
	}
	defer db.Close()
	if err := checkSchemaRO(db, args[0]); err != nil {
		return mapErr(err)
	}
	rows, err := db.Query(
		"SELECT id, name, kind, status, started_at, COALESCE(finished_at, '') " +
			"FROM mundane_steps ORDER BY id",
	)
	if err != nil {
		return die(1, "%v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name, kind, status, started, finished string
		if err := rows.Scan(&id, &name, &kind, &status, &started, &finished); err != nil {
			return die(1, "%v", err)
		}
		fmt.Printf("%d  %s  %s  %s  %s  %s\n", id, name, kind, status, started, finished)
	}
	return 0
}

func cmdGet(args []string) int {
	if len(args) != 2 {
		return die(2, "usage: mundane get <task.db> <name>")
	}
	db, err := openRO(args[0])
	if err != nil {
		return die(1, "%v", err)
	}
	defer db.Close()
	if err := checkSchemaRO(db, args[0]); err != nil {
		return mapErr(err)
	}
	var status string
	var raw []byte
	err = db.QueryRow(
		"SELECT result, status FROM mundane_steps WHERE name = ?", args[1],
	).Scan(&raw, &status)
	if err == sql.ErrNoRows {
		return die(1, "no step named %q", args[1])
	}
	if err != nil {
		return die(1, "%v", err)
	}
	if status != mundane.StatusDone {
		return die(1, "step %q is %s, not done", args[1], status)
	}
	// Both encodings (json text, raw bytes) emit verbatim.
	_, _ = os.Stdout.Write(raw)
	return 0
}
