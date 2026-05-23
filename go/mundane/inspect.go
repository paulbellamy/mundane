package mundane

import (
	"database/sql"
	"fmt"
)

type Status struct {
	Path      string
	TaskID    string
	CreatedAt string
	Total     int
	Done      int
	Pending   int
	Failed    int
}

type InspectStepRow struct {
	ID         int64
	Name       string
	Kind       string
	Encoding   string
	Status     string
	StartedAt  string
	FinishedAt string
	Error      string
}

func openRO(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	return db, nil
}

func checkSchemaRO(db *sql.DB, path string) error {
	var v string
	err := db.QueryRow("SELECT value FROM mundane_meta WHERE key='schema_version'").Scan(&v)
	if err != nil {
		return fmt.Errorf("schema_version not found in %s: %w", path, err)
	}
	if v != SchemaVersion {
		return &SchemaError{Path: path, Version: v}
	}
	return nil
}

func GetStatus(path string) (*Status, error) {
	db, err := openRO(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := checkSchemaRO(db, path); err != nil {
		return nil, err
	}
	st := &Status{Path: path}
	_ = db.QueryRow("SELECT value FROM mundane_meta WHERE key='task_id'").Scan(&st.TaskID)
	_ = db.QueryRow("SELECT value FROM mundane_meta WHERE key='created_at'").Scan(&st.CreatedAt)
	rows, err := db.Query("SELECT status, COUNT(*) FROM mundane_steps GROUP BY status")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		var c int
		if err := rows.Scan(&s, &c); err != nil {
			return nil, err
		}
		switch s {
		case StatusDone:
			st.Done = c
		case StatusPending:
			st.Pending = c
		case StatusFailed:
			st.Failed = c
		}
		st.Total += c
	}
	return st, nil
}

func GetSteps(path string) ([]InspectStepRow, error) {
	db, err := openRO(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := checkSchemaRO(db, path); err != nil {
		return nil, err
	}
	rows, err := db.Query(
		"SELECT id, name, kind, encoding, status, started_at, " +
			"COALESCE(finished_at, ''), COALESCE(error, '') " +
			"FROM mundane_steps ORDER BY id",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InspectStepRow
	for rows.Next() {
		var r InspectStepRow
		if err := rows.Scan(&r.ID, &r.Name, &r.Kind, &r.Encoding, &r.Status, &r.StartedAt, &r.FinishedAt, &r.Error); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetResult returns the cached payload for `name`. The returned bytes are the
// emit-ready form: text/b64-decoded bytes for text/b64 steps; raw JSON text
// for json steps; ASCII integer for epoch rows.
func GetResult(path, name string) (encoding string, payload []byte, err error) {
	db, err := openRO(path)
	if err != nil {
		return "", nil, err
	}
	defer db.Close()
	if err := checkSchemaRO(db, path); err != nil {
		return "", nil, err
	}
	var enc, status string
	var raw []byte
	row := db.QueryRow(
		"SELECT encoding, result, status FROM mundane_steps WHERE name = ?",
		name,
	)
	if err := row.Scan(&enc, &raw, &status); err != nil {
		if err == sql.ErrNoRows {
			return "", nil, fmt.Errorf("no step named %q", name)
		}
		return "", nil, err
	}
	if status != StatusDone {
		return "", nil, fmt.Errorf("step %q is %s, not done", name, status)
	}
	return enc, raw, nil
}
