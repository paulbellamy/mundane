// Conformance driver (Go). Reads a scenario JSON and replays its operations
// against a task DB via the SDK. The shared harness (conformance/run.py)
// invokes one driver per runtime and asserts identical on-disk state.
//
// Usage: conformance-driver <task.db> <scenario.json>
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/paulbellamy/mundane/go/mundane"
)

type operation struct {
	Op       string          `json:"op"`
	Name     string          `json:"name"`
	Value    json.RawMessage `json:"value"`
	Duration string          `json:"duration"`
}

type scenario struct {
	Operations []operation `json:"operations"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: conformance-driver <task.db> <scenario.json>")
		os.Exit(2)
	}
	dbPath, scnPath := os.Args[1], os.Args[2]

	data, err := os.ReadFile(scnPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var s scenario
	if err := json.Unmarshal(data, &s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	err = mundane.Run(dbPath, func(ctx *mundane.Ctx) error {
		for _, o := range s.Operations {
			switch o.Op {
			case "step":
				var v any
				if len(o.Value) > 0 {
					if err := json.Unmarshal(o.Value, &v); err != nil {
						return err
					}
				}
				if _, err := mundane.Step(ctx, o.Name, func() (any, error) { return v, nil }); err != nil {
					return err
				}
			case "sleep":
				if err := ctx.Sleep(o.Name, o.Duration); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unknown op: %s", o.Op)
			}
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
