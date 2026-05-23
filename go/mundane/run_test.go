package mundane

import (
	"errors"
	"path/filepath"
	"testing"
)

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
