/**
 * Cross-process exclusive lock interoperable with flock(2).
 *
 * We open the SQLite file and take flock(LOCK_EX | LOCK_NB) on its fd via the
 * `fs-ext` native binding — no subprocess. This is the same flock(2) that the
 * bash (`flock`), Go (`unix.Flock`), and Python (`fcntl.flock`) runtimes use,
 * so a lock held by any one is visible to all. The kernel drops it
 * automatically if the process dies, so there is nothing to clean up on crash.
 *
 * We hold a raw integer fd (not a `FileHandle`) on purpose: a `FileHandle` has
 * a GC finalizer that would close the fd — and release the lock — if it were
 * collected mid-run. A raw fd stays open until we close it (or the process
 * exits).
 */

import { constants, close as fsClose, open as fsOpen } from "node:fs";
import { flock } from "fs-ext";
import { MundaneLockedError } from "./errors";

export interface AcquiredLock {
  release(): Promise<void>;
}

function openFd(path: string): Promise<number> {
  return new Promise((resolve, reject) => {
    fsOpen(path, constants.O_RDWR | constants.O_CREAT, 0o644, (err, fd) => {
      if (err) reject(err);
      else resolve(fd);
    });
  });
}

function closeFd(fd: number): Promise<void> {
  return new Promise((resolve) => {
    // Closing drops the flock; ignore close errors (fd may already be gone).
    fsClose(fd, () => resolve());
  });
}

function flockExclusiveNonblock(fd: number): Promise<void> {
  return new Promise((resolve, reject) => {
    flock(fd, "exnb", (err) => {
      if (err) reject(err);
      else resolve();
    });
  });
}

export async function acquireLock(path: string): Promise<AcquiredLock> {
  const fd = await openFd(path);
  try {
    await flockExclusiveNonblock(fd);
  } catch (e) {
    await closeFd(fd);
    const code = (e as NodeJS.ErrnoException).code;
    if (code === "EWOULDBLOCK" || code === "EAGAIN") {
      throw new MundaneLockedError(`${path}: locked by another process`);
    }
    throw e;
  }

  let released = false;
  return {
    async release(): Promise<void> {
      if (released) return;
      released = true;
      await closeFd(fd);
    },
  };
}
