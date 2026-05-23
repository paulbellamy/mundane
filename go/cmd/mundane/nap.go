package main

import (
	"time"

	"github.com/paulbellamy/mundane/go/mundane"
)

// cmdNap implements: mundane __nap <db> <name> <duration>
//
// On first call: writes an epoch row (wake_at = now + duration), then sleeps
// the duration. On resume: reads the cached wake_at and sleeps only the
// remainder.
func cmdNap(args []string) int {
	if mundane.LockFDFromEnv() < 0 {
		return die(2, `__nap requires MUNDANE_LOCK_FD (run via eval "$(mundane init <db>)")`)
	}
	if len(args) != 3 {
		return die(2, "usage: __nap <db> <name> <duration>")
	}
	dbPath := args[0]
	name := args[1]
	dur := args[2]

	ms, err := mundane.ParseDurationMs(dur)
	if err != nil {
		return die(2, "%v", err)
	}

	err = mundane.RunAdoptCLI(dbPath, func(ctx *mundane.Ctx) error {
		wakeAt, hit, err := ctx.SleepRow(name)
		if err != nil {
			return err
		}
		if !hit {
			wakeAt = mundane.NowMs() + ms
			if err := ctx.WriteSleepRow(name, wakeAt); err != nil {
				return err
			}
		}
		remaining := wakeAt - mundane.NowMs()
		if remaining > 0 {
			time.Sleep(time.Duration(remaining) * time.Millisecond)
		}
		return nil
	})
	if err != nil {
		return mapErr(err)
	}
	return 0
}
