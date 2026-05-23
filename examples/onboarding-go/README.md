# Onboarding drip (Go) — naps that survive a crash

A signup drip campaign: send a welcome email, wait a few days, send tips, wait,
check in, wait, nudge to upgrade. Each **send is a step** (so it never
double-sends) and each **gap is a `nap`** (so the wait survives a crash — kill
the process during a wait and re-invoke, and it sleeps only the time that was
left, skipping mail it already sent).

The mailer is a local stub that logs only when it actually sends, so a resumed
run shows exactly which emails were *not* re-sent. Durations are shortened to
seconds so the demo finishes quickly.

## Run it

```sh
go mod tidy        # resolves the local SDK via the replace directive
go run . drip.db
#   [email] -> alice@example.com: "Welcome to Acme!"
#   ... (3s nap) ...
#   [email] -> alice@example.com: "3 tips to get started"
#   ...
#   >> drip complete
```

## Kill it mid-wait, then resume

```sh
rm -f drip.db
timeout 4 go run . drip.db     # sends welcome + tips, dies during a nap
#   [email] -> alice@example.com: "Welcome to Acme!"
#   [email] -> alice@example.com: "3 tips to get started"

go run . drip.db               # resumes — no re-sends of welcome/tips
#   [email] -> alice@example.com: "How's it going?"
#   [email] -> alice@example.com: "Ready to upgrade?"
#   >> drip complete

go run . drip.db               # everything cached, zero email lines
#   >> drip complete
```

## Inspect the durable state

```sh
../../go/mundane-bin steps drip.db   # send-* rows are 'step', wait-* are 'sleep'
```

## Notes

- mundane naps block the process (SPEC §9). This demo just lets the process
  sleep. For genuine multi-day drips, run it under a supervisor, or recron it
  so each invocation resumes the current nap's remainder.
- `Step[T]` JSON-round-trips the returned value. Because the check re-encodes
  through a sorted-key map, struct fields must be declared in the same order as
  their JSON keys sort — see the `Receipt` struct in `main.go`.
- Set `DRIP_WAIT` (e.g. `DRIP_WAIT=50ms`) to override every gap — handy for a
  fast demo or a smoke test.
