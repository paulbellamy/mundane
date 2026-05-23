/**
 * Cross-process exclusive lock interoperable with flock(2).
 *
 * Node has no built-in flock binding, so we spawn a tiny `sh` helper that
 * holds an flock(LOCK_EX | LOCK_NB) on the SQLite file's fd for the
 * duration of our process. The helper releases the lock when we close
 * its stdin (or die), thanks to the kernel auto-release of flock on
 * process exit.
 *
 * This means TS interops with bash (`flock`) and Python (`fcntl.flock`)
 * holding the *same* lock on the *same* file. They all see each other.
 *
 * The helper is portable POSIX: requires `sh` and `flock` on PATH.
 */

import { type ChildProcess, spawn } from "node:child_process";
import { MundaneLockedError } from "./errors";

const HELPER_SCRIPT = `
exec 9<>"$1" || { echo OPEN_FAILED >&2; exit 76; }
flock -nx 9 || exit 75
echo READY
# Hold the lock until stdin closes (parent exits or sends EOF).
cat > /dev/null
`;

export interface AcquiredLock {
  release(): Promise<void>;
}

export function acquireLock(path: string): Promise<AcquiredLock> {
  return new Promise<AcquiredLock>((resolve, reject) => {
    const child: ChildProcess = spawn("sh", ["-c", HELPER_SCRIPT, "--", path], {
      stdio: ["pipe", "pipe", "pipe"],
    });

    let stdout = "";
    let stderr = "";
    let settled = false;
    const settle = (fn: () => void) => {
      if (!settled) {
        settled = true;
        fn();
      }
    };

    child.stdout!.on("data", (chunk: Buffer) => {
      stdout += chunk.toString("utf8");
      if (stdout.includes("READY")) {
        settle(() => resolve(makeAcquired(child)));
      }
    });
    child.stderr!.on("data", (chunk: Buffer) => {
      stderr += chunk.toString("utf8");
    });
    child.on("error", (err) => {
      settle(() => reject(err));
    });
    child.on("exit", (code, signal) => {
      if (settled) return;
      if (code === 75) {
        settle(() => reject(new MundaneLockedError(`${path}: locked by another process`)));
        return;
      }
      settle(() =>
        reject(
          new Error(
            `lock helper exited unexpectedly (code=${code}, signal=${signal}): ${stderr.trim()}`,
          ),
        ),
      );
    });
  });
}

function makeAcquired(child: ChildProcess): AcquiredLock {
  let released = false;
  return {
    release(): Promise<void> {
      if (released) return Promise.resolve();
      released = true;
      return new Promise<void>((resolve) => {
        let resolved = false;
        const done = () => {
          if (!resolved) {
            resolved = true;
            resolve();
          }
        };
        child.once("exit", done);
        try {
          child.stdin!.end();
        } catch {
          // ignore: child may already be gone
        }
        // Backups so release() can never hang: SIGTERM at 500ms, then SIGKILL
        // (and resolve regardless) at 1500ms if the helper still hasn't exited.
        const term = setTimeout(() => {
          try {
            child.kill("SIGTERM");
          } catch {
            // ignore
          }
        }, 500);
        // SIGKILL forces the helper to exit, which fires "exit" (and drops the
        // flock). We resolve on that real exit — never on this timer — so the
        // lock is only reported released once the OS has actually dropped it.
        const kill = setTimeout(() => {
          try {
            child.kill("SIGKILL");
          } catch {
            // ignore
          }
        }, 1500);
        child.once("exit", () => {
          clearTimeout(term);
          clearTimeout(kill);
        });
      });
    },
  };
}
