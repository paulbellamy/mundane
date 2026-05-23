// A user-onboarding drip campaign built on mundane.
//
// On signup we send a sequence of emails spaced out over days. Each send is a
// step (so a resumed run never double-sends), and each gap is a nap (so the
// wait survives a crash: kill the process during a wait and re-invoke — it
// sleeps only the time that was left and skips the mail it already sent).
//
// Durations are short here so the demo finishes in seconds; each stage's
// comment shows the real-world spacing. mundane naps block the process
// (SPEC §9): for genuine multi-day drips you'd run this under a supervisor, or
// recron it so each invocation resumes the current nap's remainder.
//
//	go run . onboarding.db
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/paulbellamy/mundane/go/mundane"
)

// Fields are ordered to match their JSON keys alphabetically: mundane's
// round-trip check re-encodes through a map (sorted keys), so a struct whose
// field order differs would fail the check (SPEC §10).
type Receipt struct {
	SentAt  string `json:"sent_at"`
	Subject string `json:"subject"`
	To      string `json:"to"`
}

// sendEmail is a fake mailer: it "sends" (and logs) only when actually
// invoked, so a resumed run shows no re-sends for mail already delivered.
func sendEmail(to, subject string) (Receipt, error) {
	fmt.Printf("  [email] -> %s: %q\n", to, subject)
	return Receipt{
		To:      to,
		Subject: subject,
		SentAt:  time.Now().UTC().Format(time.RFC3339),
	}, nil
}

type stage struct {
	name      string
	subject   string
	waitAfter string // gap before the NEXT email; "" on the last stage
	realWorld string // what you'd actually use in production
}

func main() {
	db := "onboarding.db"
	if len(os.Args) > 1 {
		db = os.Args[1]
	}
	const user = "alice@example.com"

	stages := []stage{
		{"welcome", "Welcome to Acme!", "3s", "right away, then wait 3d"},
		{"tips", "3 tips to get started", "3s", "wait 4d"},
		{"checkin", "How's it going?", "4s", "wait 7d"},
		{"nudge", "Ready to upgrade?", "", "final touch"},
	}

	err := mundane.Run(db, func(ctx *mundane.Ctx) error {
		for _, s := range stages {
			if _, err := mundane.Step(ctx, "send-"+s.name, func() (Receipt, error) {
				return sendEmail(user, s.subject)
			}); err != nil {
				return err
			}
			if s.waitAfter != "" {
				if err := ctx.Sleep("wait-after-"+s.name, s.waitAfter); err != nil {
					return err
				}
			}
		}
		fmt.Println(">> drip complete")
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
