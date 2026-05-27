package mundane

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type StepRow struct {
	ID       int64
	Name     string
	Kind     string
	Encoding string
	Result   []byte
	Status   string
	Err      string
}

// Ctx is the workflow context passed to a body running under Run.
type Ctx struct {
	db    *sql.DB
	cache map[string]*StepRow
	seen  map[string]struct{}
}

func newCtx(db *sql.DB) (*Ctx, error) {
	c := &Ctx{
		db:    db,
		cache: make(map[string]*StepRow),
		seen:  make(map[string]struct{}),
	}
	if err := c.loadCache(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Ctx) loadCache() error {
	rows, err := c.db.Query(
		"SELECT id, name, kind, encoding, result, status, COALESCE(error, '') " +
			"FROM mundane_steps ORDER BY id",
	)
	if err != nil {
		return fmt.Errorf("load cache: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r StepRow
		var raw []byte
		if err := rows.Scan(&r.ID, &r.Name, &r.Kind, &r.Encoding, &raw, &r.Status, &r.Err); err != nil {
			return fmt.Errorf("scan step row: %w", err)
		}
		r.Result = raw
		c.cache[r.Name] = &r
	}
	return rows.Err()
}

func (c *Ctx) checkSeen(name string) error {
	if _, dup := c.seen[name]; dup {
		return &DuplicateStepError{Name: name}
	}
	c.seen[name] = struct{}{}
	return nil
}

// Step runs fn on cache miss and stores the JSON-encoded result. On cache hit,
// it returns the decoded cached value without calling fn. Duplicate names
// within one Run invocation return *DuplicateStepError.
//
// fn's return value must survive a JSON round-trip; on first write the runtime
// checks and returns *SerializationError if not.
func Step[T any](ctx *Ctx, name string, fn func() (T, error)) (T, error) {
	var zero T
	if err := ValidateName(name); err != nil {
		return zero, err
	}
	if err := ctx.checkSeen(name); err != nil {
		return zero, err
	}
	if cached, ok := ctx.cache[name]; ok && cached.Status == StatusDone {
		if cached.Encoding == EncJSON {
			// Decode the stored JSON straight into T (not via `any`), so int64
			// precision survives the round-trip just as it does on write.
			var out T
			if err := json.Unmarshal(cached.Result, &out); err != nil {
				return zero, fmt.Errorf("decode cached step %q: %w", name, err)
			}
			return out, nil
		}
		v, err := DecodeResult(cached.Encoding, cached.Result)
		if err != nil {
			return zero, fmt.Errorf("decode cached step %q: %w", name, err)
		}
		out, err := remarshal[T](v)
		if err != nil {
			return zero, fmt.Errorf("remarshal cached step %q into target type: %w", name, err)
		}
		return out, nil
	}
	if err := ensurePending(ctx.db, ctx.cache, name, KindStep, EncJSON); err != nil {
		return zero, err
	}
	value, err := fn()
	if err != nil {
		_ = commitFailed(ctx.db, name, err.Error())
		if r, ok := ctx.cache[name]; ok {
			r.Status = StatusFailed
			r.Err = err.Error()
		}
		return zero, &StepFailedError{Name: name, Err: err}
	}
	text, err := CheckJSONRoundtrip(value)
	if err != nil {
		_ = commitFailed(ctx.db, name, err.Error())
		return zero, err
	}
	if err := commitDone(ctx.db, ctx.cache, name, EncJSON, []byte(text)); err != nil {
		return zero, err
	}
	// Return the round-tripped value (decoded into T) so the first run and a
	// later cache hit yield identical values, even for `any`-typed results.
	var out T
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return zero, fmt.Errorf("decode step %q result: %w", name, err)
	}
	return out, nil
}

// Sleep pauses the workflow until `duration` has elapsed since the first time
// this name was hit. On resume, sleeps only the remainder.
func (c *Ctx) Sleep(name, duration string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if err := c.checkSeen(name); err != nil {
		return err
	}
	var wakeAt int64
	if cached, ok := c.cache[name]; ok && cached.Status == StatusDone {
		// Resume: the duration arg is ignored (SPEC §6), so don't parse it —
		// a now-invalid duration string must not fail an otherwise-no-op resume.
		w, err := parseEpoch(cached.Result)
		if err != nil {
			return fmt.Errorf("decode sleep row %q: %w", name, err)
		}
		wakeAt = w
	} else {
		ms, err := ParseDurationMs(duration)
		if err != nil {
			return err
		}
		wakeAt = NowMs() + ms
		if err := ensurePending(c.db, c.cache, name, KindSleep, EncJSON); err != nil {
			return err
		}
		if err := commitDone(c.db, c.cache, name, EncJSON, []byte(strconv.FormatInt(wakeAt, 10))); err != nil {
			return err
		}
	}
	remaining := wakeAt - NowMs()
	if remaining > 0 {
		time.Sleep(time.Duration(remaining) * time.Millisecond)
	}
	return nil
}

// LookupCachedStep returns the emit-ready payload bytes for a previously-
// committed step named `name`, with hit=true. Used by the CLI's __step path.
// Returns hit=false if the row is missing or not yet done. Both encodings
// (json text, raw bytes) are emitted verbatim.
func (c *Ctx) LookupCachedStep(name string) (payload []byte, hit bool, err error) {
	cached, ok := c.cache[name]
	if !ok || cached.Status != StatusDone {
		return nil, false, nil
	}
	return append([]byte(nil), cached.Result...), true, nil
}

// WriteBytesStep records a fresh bytes-encoded step row. `payload` is the raw
// stdout captured from a subprocess, stored verbatim. Used by the CLI's
// __step path.
func (c *Ctx) WriteBytesStep(name string, payload []byte) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if err := ensurePending(c.db, c.cache, name, KindStep, EncBytes); err != nil {
		return err
	}
	return commitDone(c.db, c.cache, name, EncBytes, payload)
}

// MarkStepFailed records a failure for a previously-pending step row.
func (c *Ctx) MarkStepFailed(name, msg string) error {
	// Make sure a row exists so the UPDATE has something to hit.
	_ = ensurePending(c.db, c.cache, name, KindStep, EncBytes)
	return commitFailed(c.db, name, msg)
}

// SleepRow returns the cached wake-time for a sleep row, or (0, false) if
// the row is missing or pending. Used by the CLI's __nap path.
func (c *Ctx) SleepRow(name string) (wakeAtMs int64, hit bool, err error) {
	cached, ok := c.cache[name]
	if !ok || cached.Status != StatusDone {
		return 0, false, nil
	}
	w, err := parseEpoch(cached.Result)
	if err != nil {
		return 0, false, err
	}
	return w, true, nil
}

// WriteSleepRow records a fresh sleep row with the given wake time, stored as
// a JSON number.
func (c *Ctx) WriteSleepRow(name string, wakeAtMs int64) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if err := ensurePending(c.db, c.cache, name, KindSleep, EncJSON); err != nil {
		return err
	}
	return commitDone(c.db, c.cache, name, EncJSON,
		[]byte(strconv.FormatInt(wakeAtMs, 10)))
}

// parseEpoch reads a sleep row's JSON-number payload as integer milliseconds.
// SPEC §2 only promises "a JSON number", so accept any numeric form (e.g. a
// float or exponent written by another runtime) and truncate to ms.
func parseEpoch(raw []byte) (int64, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
	if err != nil {
		return 0, err
	}
	return int64(f), nil
}

func ensurePending(db *sql.DB, cache map[string]*StepRow, name, kind, encoding string) error {
	if existing, ok := cache[name]; ok {
		// Re-running a leftover pending/failed row (this path is never reached
		// for a 'done' row). Reset it to pending so the on-disk state reflects
		// the retry rather than a stale failure (SPEC §2).
		if _, err := db.Exec(
			"UPDATE mundane_steps SET kind=?, encoding=?, status='pending', result=NULL, error=NULL, finished_at=NULL WHERE name=?",
			kind, encoding, name,
		); err != nil {
			return fmt.Errorf("reset pending: %w", err)
		}
		existing.Status = StatusPending
		existing.Kind = kind
		existing.Encoding = encoding
		existing.Result = nil
		existing.Err = ""
		return nil
	}
	now := IsoNow()
	if _, err := db.Exec(
		"INSERT OR IGNORE INTO mundane_steps (name, kind, encoding, status, started_at) "+
			"VALUES (?, ?, ?, 'pending', ?)",
		name, kind, encoding, now,
	); err != nil {
		return fmt.Errorf("insert pending: %w", err)
	}
	row := db.QueryRow(
		"SELECT id, name, kind, encoding, result, status, COALESCE(error, '') "+
			"FROM mundane_steps WHERE name = ?",
		name,
	)
	var r StepRow
	var raw []byte
	if err := row.Scan(&r.ID, &r.Name, &r.Kind, &r.Encoding, &raw, &r.Status, &r.Err); err != nil {
		return fmt.Errorf("read back pending row: %w", err)
	}
	r.Result = raw
	cache[name] = &r
	return nil
}

func commitDone(db *sql.DB, cache map[string]*StepRow, name, encoding string, result []byte) error {
	now := IsoNow()
	if _, err := db.Exec(
		"UPDATE mundane_steps SET status='done', encoding=?, result=?, finished_at=?, error=NULL "+
			"WHERE name=?",
		encoding, result, now, name,
	); err != nil {
		return fmt.Errorf("commit done: %w", err)
	}
	if r, ok := cache[name]; ok {
		r.Status = StatusDone
		r.Encoding = encoding
		r.Result = append([]byte(nil), result...)
	}
	return nil
}

func commitFailed(db *sql.DB, name, errMsg string) error {
	now := IsoNow()
	_, err := db.Exec(
		"UPDATE mundane_steps SET status='failed', error=?, finished_at=? WHERE name=?",
		errMsg, now, name,
	)
	return err
}

// remarshal converts a generic decoded JSON value into the caller's target type T.
func remarshal[T any](v any) (T, error) {
	var zero T
	text, err := json.Marshal(v)
	if err != nil {
		return zero, err
	}
	var out T
	if err := json.Unmarshal(text, &out); err != nil {
		return zero, err
	}
	return out, nil
}
