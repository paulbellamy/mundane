package mundane

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Run opens the SQLite file at path, takes an exclusive flock, bootstraps the
// schema if missing, builds the in-memory cache, and invokes fn. The lock is
// released on return.
//
// If MUNDANE_LOCK_FD is set in the environment, Run adopts the parent's lock
// instead of acquiring its own (used by the CLI's __step/__nap subcommands).
//
// Returns *LockedError on contention, *SchemaError on version mismatch, and
// whatever fn returns otherwise.
func Run(path string, fn func(*Ctx) error) error {
	return run(path, false, fn)
}

func run(path string, skipBootstrap bool, fn func(*Ctx) error) error {
	var lock *FileLock
	var err error
	if fd := LockFDFromEnv(); fd >= 0 {
		lock = AdoptLockFromFD(fd)
	} else {
		lock, err = AcquireLock(path)
		if err != nil {
			return err
		}
		defer lock.Release()
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if !skipBootstrap {
		if err := Bootstrap(db, path); err != nil {
			return err
		}
	}

	ctx, err := newCtx(db)
	if err != nil {
		return err
	}
	return fn(ctx)
}

// runAdopt is the CLI-only variant that always adopts the parent lock and
// skips bootstrap (parent already did it).
func RunAdoptCLI(path string, fn func(*Ctx) error) error {
	return run(path, true, fn)
}
