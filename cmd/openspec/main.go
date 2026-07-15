// Command openspec is a spec-driven development CLI for AI coding agents.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is stamped via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	root := &cobra.Command{
		Use:           "openspec",
		Short:         "Spec-driven development for AI coding agents",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		newInitCmd(),
		newUpdateCmd(),
		newNewCmd(),
		newStatusCmd(),
		newInstructionsCmd(),
		newListCmd(),
		newValidateCmd(),
		newArchiveCmd(),
		newTeamCmd(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// statusEntry is the --json failure contract: one document, exit code 1.
type statusEntry struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Fix      string `json:"fix,omitempty"`
}

func printJSON(v any) error {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

// failJSON prints the failure payload and exits 1 without double-printing.
func failJSON(payload map[string]any, code string, err error) error {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["status"] = []statusEntry{{Severity: "error", Code: code, Message: err.Error()}}
	printJSON(payload)
	os.Exit(1)
	return nil
}

func fail(jsonMode bool, payload map[string]any, code string, err error) error {
	if jsonMode {
		return failJSON(payload, code, err)
	}
	return err
}
