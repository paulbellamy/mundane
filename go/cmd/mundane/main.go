package main

import (
	"fmt"
	"os"
)

const usage = `usage: mundane <subcommand> [args]
  init   <task.db>           emit sh init script (eval to use step/nap)
  status <task.db>
  steps  <task.db>
  get    <task.db> <name>

  __bootstrap <task.db>            (internal; used by init)
  __step [--b64] <task.db> <name> -- CMD [args...]
                                   (internal; called by step shell function)
  __nap  <task.db> <name> <duration>
                                   (internal; called by nap shell function)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	var exit int
	switch cmd {
	case "init":
		exit = cmdInit(args)
	case "status":
		exit = cmdStatus(args)
	case "steps":
		exit = cmdSteps(args)
	case "get":
		exit = cmdGet(args)
	case "__bootstrap":
		exit = cmdBootstrap(args)
	case "__step":
		exit = cmdStep(args)
	case "__nap":
		exit = cmdNap(args)
	case "-h", "--help", "help":
		fmt.Fprint(os.Stdout, usage)
		exit = 0
	default:
		fmt.Fprintf(os.Stderr, "mundane: unknown subcommand: %s\n", cmd)
		fmt.Fprint(os.Stderr, usage)
		exit = 2
	}
	os.Exit(exit)
}

func die(code int, format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "mundane: "+format+"\n", args...)
	return code
}
