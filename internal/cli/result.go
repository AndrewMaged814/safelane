package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

// ResultEnvelope is the stable machine contract emitted by SafeLane commands.
type ResultEnvelope struct {
	SchemaVersion string         `json:"schema_version"`
	Command       string         `json:"command"`
	OK            bool           `json:"ok"`
	ReleaseID     string         `json:"release_id,omitempty"`
	State         string         `json:"state"`
	NextCommand   string         `json:"next_command"`
	Warnings      []string       `json:"warnings"`
	Result        map[string]any `json:"result"`
}

func WriteResult(w io.Writer, command, state, next string, result map[string]any) error {
	if result == nil {
		result = map[string]any{}
	}
	return json.NewEncoder(w).Encode(ResultEnvelope{
		SchemaVersion: "safelane.command.result/v1",
		Command:       command,
		OK:            true,
		State:         state,
		NextCommand:   next,
		Warnings:      []string{},
		Result:        result,
	})
}

func writeResultError(stderr io.Writer, command string, err error) int {
	fmt.Fprintf(stderr, "safelane %s: %v\n", command, err)
	return ExitFail
}
