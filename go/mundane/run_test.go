package mundane

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStepAnyResultStableAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.db")

	var first, hit any
	if err := Run(path, func(ctx *Ctx) error {
		v, err := Step(ctx, "s", func() (any, error) { return map[string]any{"n": 42}, nil })
		first = v
		return err
	}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := Run(path, func(ctx *Ctx) error {
		v, err := Step(ctx, "s", func() (any, error) { return nil, nil })
		hit = v
		return err
	}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	// First-run and cache-hit must agree even for `any`-typed results (both
	// decode the stored JSON, so numbers are float64 on both sides).
	if !reflect.DeepEqual(first, hit) {
		t.Errorf("first run %#v != cache hit %#v", first, hit)
	}
}

func TestRunLocksDespiteLockFDEnv(t *testing.T) {
	// A stray MUNDANE_LOCK_FD in the environment must not disable locking in
	// the public Run (only the CLI's RunAdoptCLI adopts a parent lock).
	t.Setenv("MUNDANE_LOCK_FD", "7")
	dir := t.TempDir()
	path := filepath.Join(dir, "task.db")
	err := Run(path, func(*Ctx) error {
		return Run(path, func(*Ctx) error { return nil })
	})
	var le *LockedError
	if !errors.As(err, &le) {
		t.Fatalf("public Run must still acquire its own lock; got %T %v", err, err)
	}
}

func TestStepStructAndLargeIntRoundtrip(t *testing.T) {
	type User struct {
		Email string
		Name  string
		ID    int64
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "task.db")
	want := User{Email: "a@b.c", Name: "alice", ID: 9007199254740993}

	if err := Run(path, func(ctx *Ctx) error {
		got, err := Step(ctx, "fetch", func() (User, error) { return want, nil })
		if err != nil {
			return err
		}
		if got != want {
			t.Errorf("first run: got %+v, want %+v", got, want)
		}
		return nil
	}); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Resume: value comes from cache and must match exactly, including the
	// large int64 (read path decodes straight into the struct).
	if err := Run(path, func(ctx *Ctx) error {
		got, err := Step(ctx, "fetch", func() (User, error) {
			t.Error("fn must not run on cache hit")
			return User{}, nil
		})
		if err != nil {
			return err
		}
		if got != want {
			t.Errorf("resume: got %+v, want %+v", got, want)
		}
		return nil
	}); err != nil {
		t.Fatalf("resume: %v", err)
	}
}

func TestFailedStepReruns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.db")

	err := Run(path, func(ctx *Ctx) error {
		_, err := Step(ctx, "s", func() (int, error) { return 0, fmt.Errorf("boom") })
		return err
	})
	var sf *StepFailedError
	if !errors.As(err, &sf) {
		t.Fatalf("expected StepFailedError, got %T %v", err, err)
	}

	// A failed step is not cached (only 'done' is); it must re-run.
	called := 0
	if err := Run(path, func(ctx *Ctx) error {
		v, err := Step(ctx, "s", func() (int, error) { called++; return 7, nil })
		if err != nil {
			return err
		}
		if v != 7 {
			t.Errorf("rerun value: got %d, want 7", v)
		}
		return nil
	}); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if called != 1 {
		t.Errorf("fn called %d times on rerun, want 1", called)
	}
}

func TestFailedStepResetToPendingDuringRerun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.db")

	if err := Run(path, func(ctx *Ctx) error {
		_, err := Step(ctx, "s", func() (int, error) { return 0, fmt.Errorf("boom") })
		return err
	}); err == nil {
		t.Fatal("expected first run to fail")
	}

	// While the re-run body executes, the row must read 'pending', not the
	// stale 'failed' left by the previous attempt.
	var midStatus, midErr string
	if err := Run(path, func(ctx *Ctx) error {
		_, err := Step(ctx, "s", func() (int, error) {
			row := ctx.db.QueryRow("SELECT status, COALESCE(error, '') FROM mundane_steps WHERE name='s'")
			_ = row.Scan(&midStatus, &midErr)
			return 1, nil
		})
		return err
	}); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if midStatus != StatusPending {
		t.Errorf("status during re-run = %q, want %q", midStatus, StatusPending)
	}
	if midErr != "" {
		t.Errorf("error not cleared during re-run: %q", midErr)
	}
}

func TestPendingStepReruns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.db")

	// Bootstrap, then leave a pending row behind (simulating a crash mid-step).
	if err := Run(path, func(ctx *Ctx) error {
		_, err := ctx.db.Exec(
			"INSERT INTO mundane_steps (name, kind, encoding, status, started_at) " +
				"VALUES ('s', 'step', 'json', 'pending', '2020-01-01T00:00:00.000Z')")
		return err
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	called := 0
	if err := Run(path, func(ctx *Ctx) error {
		v, err := Step(ctx, "s", func() (int, error) { called++; return 5, nil })
		if err != nil {
			return err
		}
		if v != 5 {
			t.Errorf("value: got %d, want 5", v)
		}
		return nil
	}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if called != 1 {
		t.Errorf("pending step fn called %d times, want 1 (must re-run)", called)
	}
}

func TestRunBootstrapAndStep(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.db")

	called := 0
	err := Run(path, func(ctx *Ctx) error {
		v, err := Step(ctx, "greet", func() (string, error) {
			called++
			return "hello", nil
		})
		if err != nil {
			return err
		}
		if v != "hello" {
			t.Errorf("step value: got %q, want %q", v, "hello")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if called != 1 {
		t.Errorf("step fn called %d times on first run, want 1", called)
	}

	// Second run: cache hit, fn must not be called.
	called = 0
	err = Run(path, func(ctx *Ctx) error {
		v, err := Step(ctx, "greet", func() (string, error) {
			called++
			return "DIFFERENT", nil
		})
		if err != nil {
			return err
		}
		if v != "hello" {
			t.Errorf("cache value: got %q, want %q", v, "hello")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if called != 0 {
		t.Errorf("step fn called %d times on resume, want 0", called)
	}
}

func TestDuplicateStepName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.db")
	err := Run(path, func(ctx *Ctx) error {
		_, _ = Step(ctx, "x", func() (int, error) { return 1, nil })
		_, err := Step(ctx, "x", func() (int, error) { return 2, nil })
		return err
	})
	var dup *DuplicateStepError
	if !errors.As(err, &dup) {
		t.Fatalf("expected DuplicateStepError, got %T %v", err, err)
	}
	if dup.Name != "x" {
		t.Errorf("dup name: %q", dup.Name)
	}
}

func TestLockContention(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.db")

	holdErr := make(chan error, 1)
	holding := make(chan struct{})
	go func() {
		holdErr <- Run(path, func(ctx *Ctx) error {
			close(holding)
			// Hold while we try the second Run below.
			_, _ = Step(ctx, "wait", func() (int, error) { return 1, nil })
			// Spin briefly so the second goroutine has time to attempt.
			for i := 0; i < 10; i++ {
				if i == 5 {
					// Yield to let the second attempt run.
					_, _ = Step(ctx, "spin", func() (int, error) { return i, nil })
				}
			}
			return nil
		})
	}()
	<-holding

	err := Run(path, func(ctx *Ctx) error { return nil })
	var le *LockedError
	if !errors.As(err, &le) {
		t.Errorf("expected LockedError while held, got %T %v", err, err)
	}
	if err := <-holdErr; err != nil {
		t.Fatalf("holder run: %v", err)
	}
}

func TestInvalidName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.db")
	err := Run(path, func(ctx *Ctx) error {
		_, err := Step(ctx, "has space", func() (int, error) { return 0, nil })
		return err
	})
	var ne *InvalidNameError
	if !errors.As(err, &ne) {
		t.Fatalf("expected InvalidNameError, got %T %v", err, err)
	}
}

func TestSleepEpoch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.db")
	err := Run(path, func(ctx *Ctx) error {
		return ctx.Sleep("brief", "1ms")
	})
	if err != nil {
		t.Fatalf("sleep run: %v", err)
	}

	// Resume: row exists, second invocation should be near-instant and still succeed.
	err = Run(path, func(ctx *Ctx) error {
		return ctx.Sleep("brief", "10s") // duration arg ignored on cache hit
	})
	if err != nil {
		t.Fatalf("sleep resume: %v", err)
	}
}

func TestSleepResumeIgnoresInvalidDuration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.db")
	if err := Run(path, func(ctx *Ctx) error { return ctx.Sleep("n", "1ms") }); err != nil {
		t.Fatalf("first sleep: %v", err)
	}
	// Resume: the duration arg is ignored, so a now-invalid string must not
	// turn an otherwise-no-op resume into an error.
	if err := Run(path, func(ctx *Ctx) error { return ctx.Sleep("n", "not-a-duration") }); err != nil {
		t.Fatalf("resume must ignore the invalid duration arg, got %v", err)
	}
}

func TestSchemaMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.db")
	// Bootstrap once normally.
	if err := Run(path, func(ctx *Ctx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	// Corrupt schema_version.
	err := Run(path, func(ctx *Ctx) error {
		_, err := ctx.db.Exec("UPDATE mundane_meta SET value='99' WHERE key='schema_version'")
		return err
	})
	if err != nil {
		t.Fatalf("corruption setup: %v", err)
	}

	// Next open must fail with SchemaError.
	err = Run(path, func(ctx *Ctx) error { return nil })
	var se *SchemaError
	if !errors.As(err, &se) {
		t.Fatalf("expected SchemaError, got %T %v", err, err)
	}
}
