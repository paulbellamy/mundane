package main

import (
	"fmt"
	"os"

	"github.com/paulbellamy/mundane/go/mundane"
)

func cmdStatus(args []string) int {
	if len(args) != 1 {
		return die(2, "usage: mundane status <task.db>")
	}
	st, err := mundane.GetStatus(args[0])
	if err != nil {
		return mapErr(err)
	}
	fmt.Printf("task_id=%s  steps=%d  done=%d  pending=%d  failed=%d\n",
		st.TaskID, st.Total, st.Done, st.Pending, st.Failed)
	return 0
}

func cmdSteps(args []string) int {
	if len(args) != 1 {
		return die(2, "usage: mundane steps <task.db>")
	}
	rows, err := mundane.GetSteps(args[0])
	if err != nil {
		return mapErr(err)
	}
	for _, r := range rows {
		fmt.Printf("%d  %s  %s  %s  %s  %s\n",
			r.ID, r.Name, r.Kind, r.Status, r.StartedAt, r.FinishedAt)
	}
	return 0
}

func cmdGet(args []string) int {
	if len(args) != 2 {
		return die(2, "usage: mundane get <task.db> <name>")
	}
	enc, payload, err := mundane.GetResult(args[0], args[1])
	if err != nil {
		return die(1, "%v", err)
	}
	switch enc {
	case mundane.EncB64:
		// emit decoded bytes
		dec, err := base64Decode(payload)
		if err != nil {
			return die(1, "decode b64: %v", err)
		}
		_, _ = os.Stdout.Write(dec)
	default:
		_, _ = os.Stdout.Write(payload)
	}
	return 0
}

func base64Decode(s []byte) ([]byte, error) {
	return mundane.B64Decode(string(s))
}
