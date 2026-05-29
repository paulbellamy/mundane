package mundane

import (
	"errors"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// FileLock holds an exclusive non-blocking flock(2) on a file.
type FileLock struct {
	path string
	fd   int
}

// AcquireLock opens the file (creating if missing) and takes LOCK_EX|LOCK_NB.
// Returns *LockedError on contention.
func AcquireLock(path string) (*FileLock, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC, 0o644)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = unix.Close(fd)
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, &LockedError{Path: path}
		}
		return nil, err
	}
	return &FileLock{path: path, fd: fd}, nil
}

// AdoptLockFromFD wraps an already-flocked fd inherited from a parent process.
// Used when MUNDANE_LOCK_FD is set: we trust the parent holds the lock; the
// child does not re-flock (which would block).
func AdoptLockFromFD(fd int) *FileLock {
	return &FileLock{path: "", fd: fd}
}

// Release unlocks and closes the lock fd. Adopted locks are unlocked by their
// owning parent on exit; the child should not Release.
func (l *FileLock) Release() {
	if l == nil || l.fd <= 0 {
		return
	}
	_ = unix.Flock(l.fd, unix.LOCK_UN)
	_ = unix.Close(l.fd)
	l.fd = -1
}

// SetCloseOnExec marks fd FD_CLOEXEC so a child process (e.g. a step's CMD)
// does not inherit the lock fd and accidentally keep the flock held after the
// shell that owns it exits.
func SetCloseOnExec(fd int) error {
	_, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, unix.FD_CLOEXEC)
	return err
}

// LockFDFromEnv parses MUNDANE_LOCK_FD; returns -1 if unset or invalid.
func LockFDFromEnv() int {
	n, err := strconv.Atoi(os.Getenv("MUNDANE_LOCK_FD"))
	if err != nil || n < 0 {
		return -1
	}
	return n
}
