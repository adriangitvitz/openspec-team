package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/adriangitvitz/openspec-team/internal/change"
	"github.com/adriangitvitz/openspec-team/internal/core"
	"github.com/adriangitvitz/openspec-team/internal/fsutil"
	"github.com/adriangitvitz/openspec-team/internal/team"
)

func newTeamCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "team",
		Short: "Persona runner for the team-driven workflow",
	}
	cmd.AddCommand(newTeamPromptCmd(), newTeamRunCmd(), newTeamToolsCmd())
	return cmd
}

// newTeamToolsCmd works outside a project: the contract is static.
func newTeamToolsCmd() *cobra.Command {
	var jsonMode bool
	cmd := &cobra.Command{
		Use:   "tools",
		Short: "Print the runner's tool inventory and needs-protocol contract for external harnesses",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			contract := team.IntegrationContract()
			if jsonMode {
				return printJSON(contract)
			}
			fmt.Printf("Advertised tools (%d):\n", len(contract.Tools))
			for _, t := range contract.Tools {
				fmt.Printf("  %-20s %s\n", t.Name, t.Precondition)
			}
			fmt.Printf("\nNeeds protocol: exit code %d pauses the run; payload on stdout; %s\n",
				contract.NeedsProtocol.ExitCode, contract.NeedsProtocol.NeedsFile)
			fmt.Printf("Round-trip cap: %d (override with %s)\n",
				contract.NeedsProtocol.RoundTripCap, contract.NeedsProtocol.CapOverride)
			fmt.Println("\nFull machine-readable contract: openspec team tools --json")
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonMode, "json", false, "Output the full contract as JSON")
	return cmd
}

func assembleForPersona(changeName, persona, artifactID string) (*team.Assembly, *change.Context, error) {
	root, err := resolveRoot()
	if err != nil {
		return nil, nil, err
	}
	ctx, err := change.LoadContext(root, changeName, "")
	if err != nil {
		return nil, nil, err
	}
	assembly, err := team.Assemble(ctx, persona, artifactID)
	if err != nil {
		return nil, nil, err
	}
	return assembly, ctx, nil
}

func newTeamPromptCmd() *cobra.Command {
	var changeName, artifactID string
	var jsonMode bool
	cmd := &cobra.Command{
		Use:   "prompt <persona>",
		Short: "Assemble a persona's prompt for one artifact (system prompt + brief + dependencies + evidence bundle)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			assembly, _, err := assembleForPersona(changeName, args[0], artifactID)
			if err != nil {
				return fail(jsonMode, nil, "assembly_error", err)
			}
			if jsonMode {
				return printJSON(assembly)
			}
			fmt.Println(team.Render(assembly))
			return nil
		},
	}
	cmd.Flags().StringVar(&changeName, "change", "", "Change name (required)")
	cmd.Flags().StringVar(&artifactID, "artifact", "", "Artifact the persona authors or reviews (required)")
	cmd.Flags().BoolVar(&jsonMode, "json", false, "Output as JSON")
	cmd.MarkFlagRequired("change")
	cmd.MarkFlagRequired("artifact")
	return cmd
}

func newTeamRunCmd() *cobra.Command {
	var changeName, artifactID string
	var write bool
	var maxToolIterations, maxExtractionRoundTrips int
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "run <persona>",
		Short: "Execute a persona against its configured external runner (openrouter)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			persona := args[0]
			assembly, ctx, err := assembleForPersona(changeName, persona, artifactID)
			if err != nil {
				return err
			}

			cfg := core.ReadProjectConfig(ctx.Root)
			runner, model := cfg.Team.PersonaRunner(persona)
			switch runner {
			case "claude":
				return fmt.Errorf("persona %q uses the claude runner: run it as a subagent via the team skill (/opsx:team). External execution is only for personas configured with runner: openrouter", persona)
			case "openrouter":
			default:
				return fmt.Errorf("persona %q has unknown runner %q (claude or openrouter); fix openspec/config.yaml", persona, runner)
			}
			if model == "" {
				return fmt.Errorf("persona %q: runner openrouter requires a model; set team.personas.%s.model in openspec/config.yaml", persona, persona)
			}
			apiKey := os.Getenv("OPENROUTER_API_KEY")
			if apiKey == "" {
				return fmt.Errorf("OPENROUTER_API_KEY is not set; export it to run openrouter personas")
			}

			artifact, _ := ctx.Schema.Artifact(artifactID)
			if write && fsutil.IsGlobPattern(artifact.Generates) {
				return fmt.Errorf("--write is not supported for multi-file artifact %q (%s); consume stdout and split the output", artifactID, artifact.Generates)
			}

			output, err := team.RunOpenRouter(team.RunnerOptions{
				Model:                   model,
				APIKey:                  apiKey,
				Root:                    ctx.Root,
				MaxToolIterations:       maxToolIterations,
				Timeout:                 timeout,
				SearchMCPURL:            cfg.Team.Search.MCPURL,
				SearchToken:             os.Getenv("OPENSPEC_SEARCH_TOKEN"),
				ChangeName:              ctx.ChangeName,
				ChangeDir:               ctx.ChangeDir,
				Persona:                 persona,
				Artifact:                artifactID,
				MaxExtractionRoundTrips: maxExtractionRoundTrips,
				Confidential:            cfg.Team.Confidential,
			}, assembly.SystemPrompt, team.Render(assembly))
			var needs *team.ExtractionNeeded
			if errors.As(err, &needs) {

				payload, jsonErr := json.MarshalIndent(needs.Payload, "", "  ")
				if jsonErr != nil {
					return jsonErr
				}
				fmt.Println(string(payload))
				fmt.Fprintf(os.Stderr, "run paused (round trip %d): fulfill the extraction request(s) above and re-run.\nPending requests: %s\n",
					needs.Payload.RoundTrip, filepath.Join(ctx.ChangeDir, team.NeedsFileName))
				os.Exit(team.NeedsExitCode)
			}
			if err != nil {
				return err
			}

			if clearErr := team.ClearPendingExtractions(ctx.ChangeDir, persona, artifactID); clearErr != nil {
				fmt.Fprintf(os.Stderr, "warning: could not clear pending extraction requests: %v\n", clearErr)
			}

			if write {
				path, err := team.WriteArtifactOutput(ctx.ChangeDir, artifactID, artifact.Generates, output)
				if err != nil {
					return err
				}
				fmt.Printf("Wrote %s\n", path)
				return nil
			}
			fmt.Println(output)
			return nil
		},
	}
	cmd.Flags().StringVar(&changeName, "change", "", "Change name (required)")
	cmd.Flags().StringVar(&artifactID, "artifact", "", "Artifact the persona authors or reviews (required)")
	cmd.Flags().BoolVar(&write, "write", false, "Write the output to the artifact's path (single-file artifacts only)")
	cmd.Flags().IntVar(&maxToolIterations, "max-tool-iterations", team.DefaultMaxToolIterations, "Tool-loop iteration bound for the external model")
	cmd.Flags().IntVar(&maxExtractionRoundTrips, "max-extraction-roundtrips", team.DefaultMaxExtractionRoundTrips, "Extraction pause/fulfill/re-run cycles allowed per persona and artifact")
	cmd.Flags().DurationVar(&timeout, "timeout", 120*time.Second, "Per-request timeout for the external model (e.g. 300s for reasoning models)")
	cmd.MarkFlagRequired("change")
	cmd.MarkFlagRequired("artifact")
	return cmd
}
