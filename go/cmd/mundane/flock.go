package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// cmdFlock implements: mundane __flock <fd>
//
// macOS does not ship flock(1); init.tmpl calls this helper instead. The
// shell opens the DB file on <fd> (via `exec NN<>db`) before invoking us;
// we inherit that fd, take flock(LOCK_EX|LOCK_NB) on it, and exit. The lock
// is on the open file description (shared with the parent shell), so
// closing our fd on exit does not release it — the shell still references
// the OFD. Exit 75 on contention, matching the rest of mundane; we stay
// silent there because init.tmpl prints its own "is locked" message.
func cmdFlock(args []string) int {
	if len(args) != 1 {
		return die(2, "usage: mundane __flock <fd>")
	}
	fd, err := strconv.Atoi(args[0])
	if err != nil || fd < 0 {
		return die(2, "invalid fd: %s", args[0])
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) {
			return 75
		}
		fmt.Fprintf(os.Stderr, "mundane: flock fd %d: %v\n", fd, err)
		return 1
	}
	return 0
}
