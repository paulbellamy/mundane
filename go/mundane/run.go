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
// Run always acquires its own lock. (The CLI's __step/__nap subcommands use
// RunAdoptCLI to adopt a parent-held lock instead.)
//
// Returns *LockedError on contention, *SchemaError on version mismatch, and
// whatever fn returns otherwise.
func Run(path string, fn func(*Ctx) error) error {
	return run(path, false, false, fn)
}

func run(path string, adoptLock, skipBootstrap bool, fn func(*Ctx) error) error {
	var lock *FileLock
	var err error
	if adoptLock {
		fd := LockFDFromEnv()
		if fd < 0 {
			return fmt.Errorf("adopt lock: MUNDANE_LOCK_FD not set")
		}
		lock = AdoptLockFromFD(fd)
	} else {
		lock, err = AcquireLock(path)
		if err != nil {
			return err
		}
		defer lock.Release()
	}
	_ = lock

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

// RunAdoptCLI is the CLI-only variant that adopts the parent lock from
// MUNDANE_LOCK_FD and skips bootstrap (parent already did it).
func RunAdoptCLI(path string, fn func(*Ctx) error) error {
	return run(path, true, true, fn)
}
