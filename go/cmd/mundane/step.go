package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"

	"github.com/paulbellamy/mundane/go/mundane"
)

// cmdStep implements: mundane __step <db> <name> -- CMD [args...]
//
// It requires MUNDANE_LOCK_FD (set by `mundane init`) and adopts the parent's
// flock. On cache miss it runs CMD, captures stdout, stores it as raw bytes,
// and emits. On cache hit it just emits the cached payload. Nonzero CMD exit
// marks the step failed and exits with that code.
func cmdStep(args []string) int {
	if mundane.LockFDFromEnv() < 0 {
		return die(2, `__step requires MUNDANE_LOCK_FD (run via eval "$(mundane init <db>)")`)
	}
	if len(args) < 4 {
		return die(2, "usage: __step <db> <name> -- CMD [args...]")
	}
	dbPath := args[0]
	name := args[1]
	if args[2] != "--" {
		return die(2, "expected '--' after name")
	}
	cmd := args[3]
	cmdArgs := args[4:]

	var stepExit int
	stepFailed := false
	err := mundane.RunAdoptCLI(dbPath, func(ctx *mundane.Ctx) error {
		payload, hit, err := ctx.LookupCachedStep(name)
		if err != nil {
			return err
		}
		if hit {
			_, _ = os.Stdout.Write(payload)
			return nil
		}
		var stdout bytes.Buffer
		c := exec.Command(cmd, cmdArgs...)
		c.Stdout = &stdout
		c.Stderr = os.Stderr
		c.Stdin = os.Stdin
		if runErr := c.Run(); runErr != nil {
			msg := fmt.Sprintf("step %q exited %v", name, runErr)
			_ = ctx.MarkStepFailed(name, msg)
			if ee, ok := runErr.(*exec.ExitError); ok {
				stepExit = ee.ExitCode()
			} else {
				stepExit = 1
			}
			fmt.Fprintf(os.Stderr, "mundane: %s\n", msg)
			stepFailed = true
			return nil
		}
		if err := ctx.WriteBytesStep(name, stdout.Bytes()); err != nil {
			return err
		}
		_, _ = os.Stdout.Write(stdout.Bytes())
		return nil
	})
	if stepFailed {
		return stepExit
	}
	if err != nil {
		return mapErr(err)
	}
	return 0
}

func mapErr(err error) int {
	switch err.(type) {
	case *mundane.LockedError:
		return die(75, "%v", err)
	case *mundane.SchemaError:
		return die(2, "%v", err)
	case *mundane.InvalidNameError:
		return die(2, "%v", err)
	}
	return die(1, "%v", err)
}
