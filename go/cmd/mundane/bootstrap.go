package main

import (
	"database/sql"

	"github.com/paulbellamy/mundane/go/mundane"
	_ "modernc.org/sqlite"
)

func cmdBootstrap(args []string) int {
	if len(args) != 1 {
		return die(2, "usage: mundane __bootstrap <task.db>")
	}
	path := args[0]
	// We expect to be called from the eval'd init script, which already holds
	// the flock on MUNDANE_LOCK_FD. We bootstrap inside that lock.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return die(1, "open: %v", err)
	}
	defer db.Close()
	if err := mundane.Bootstrap(db, path); err != nil {
		if _, ok := err.(*mundane.SchemaError); ok {
			return die(2, "%v", err)
		}
		return die(1, "%v", err)
	}
	return 0
}
