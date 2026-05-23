package mundane

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"strconv"
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
		v, err := DecodeResult(cached.Encoding, cached.Result)
		if err != nil {
			return zero, fmt.Errorf("decode cached step %q: %w", name, err)
		}
		// JSON-typed cache hit: re-marshal into T.
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
	return value, nil
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
	ms, err := ParseDurationMs(duration)
	if err != nil {
		return err
	}
	var wakeAt int64
	if cached, ok := c.cache[name]; ok && cached.Status == StatusDone {
		v, err := DecodeResult(cached.Encoding, cached.Result)
		if err != nil {
			return fmt.Errorf("decode sleep row %q: %w", name, err)
		}
		w, ok := v.(int64)
		if !ok {
			return fmt.Errorf("sleep row %q has non-epoch payload", name)
		}
		wakeAt = w
	} else {
		wakeAt = NowMs() + ms
		if err := ensurePending(c.db, c.cache, name, KindSleep, EncEpoch); err != nil {
			return err
		}
		if err := commitDone(c.db, c.cache, name, EncEpoch, []byte(strconv.FormatInt(wakeAt, 10))); err != nil {
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
// Returns hit=false if the row is missing or not yet done.
func (c *Ctx) LookupCachedStep(name string) (payload []byte, hit bool, err error) {
	cached, ok := c.cache[name]
	if !ok || cached.Status != StatusDone {
		return nil, false, nil
	}
	switch cached.Encoding {
	case EncText, EncJSON:
		return append([]byte(nil), cached.Result...), true, nil
	case EncB64:
		dec, err := base64.StdEncoding.DecodeString(string(cached.Result))
		if err != nil {
			return nil, false, fmt.Errorf("decode b64 cache: %w", err)
		}
		return dec, true, nil
	}
	return nil, false, fmt.Errorf("cannot emit step %q with encoding %q", name, cached.Encoding)
}

// WriteTextStep records a fresh text/b64 step row. `payload` is the raw bytes
// captured from a subprocess's stdout. If b64 is true, payload is stored
// base64-encoded; otherwise as text. Used by the CLI's __step path.
func (c *Ctx) WriteTextStep(name string, payload []byte, b64 bool) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	enc := EncText
	stored := payload
	if b64 {
		enc = EncB64
		stored = []byte(base64.StdEncoding.EncodeToString(payload))
	}
	if err := ensurePending(c.db, c.cache, name, KindStep, enc); err != nil {
		return err
	}
	return commitDone(c.db, c.cache, name, enc, stored)
}

// MarkStepFailed records a failure for a previously-pending step row.
func (c *Ctx) MarkStepFailed(name, msg string) error {
	// Make sure a row exists so the UPDATE has something to hit.
	_ = ensurePending(c.db, c.cache, name, KindStep, EncText)
	return commitFailed(c.db, name, msg)
}

// SleepRow returns the cached wake-time for a sleep row, or (0, false) if
// the row is missing or pending. Used by the CLI's __nap path.
func (c *Ctx) SleepRow(name string) (wakeAtMs int64, hit bool, err error) {
	cached, ok := c.cache[name]
	if !ok || cached.Status != StatusDone {
		return 0, false, nil
	}
	if cached.Encoding != EncEpoch {
		return 0, false, fmt.Errorf("sleep row %q has non-epoch encoding %q", name, cached.Encoding)
	}
	v, err := DecodeResult(cached.Encoding, cached.Result)
	if err != nil {
		return 0, false, err
	}
	w, ok := v.(int64)
	if !ok {
		return 0, false, fmt.Errorf("sleep row %q payload not int64", name)
	}
	return w, true, nil
}

// WriteSleepRow records a fresh sleep row with the given wake time.
func (c *Ctx) WriteSleepRow(name string, wakeAtMs int64) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if err := ensurePending(c.db, c.cache, name, KindSleep, EncEpoch); err != nil {
		return err
	}
	return commitDone(c.db, c.cache, name, EncEpoch,
		[]byte(strconv.FormatInt(wakeAtMs, 10)))
}

func ensurePending(db *sql.DB, cache map[string]*StepRow, name, kind, encoding string) error {
	if _, ok := cache[name]; ok {
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
	text, err := jsonMarshal(v)
	if err != nil {
		return zero, err
	}
	var out T
	if err := jsonUnmarshal(text, &out); err != nil {
		return zero, err
	}
	return out, nil
}
