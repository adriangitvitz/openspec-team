package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/adriangitvitz/openspec-go/internal/change"
	"github.com/adriangitvitz/openspec-go/internal/core"
	"github.com/adriangitvitz/openspec-go/internal/merge"
)

// captureFingerprints is best-effort: fingerprint capture must never break the command.
func captureFingerprints(ctx *change.Context) {
	_ = merge.CaptureFingerprints(ctx.ChangeDir, core.SpecsDir(ctx.Root), false)
}

// refreshChangeFingerprints is the only fingerprint overwrite path.
func refreshChangeFingerprints(ctx *change.Context) {
	_ = merge.CaptureFingerprints(ctx.ChangeDir, core.SpecsDir(ctx.Root), true)
}

func newArchiveCmd() *cobra.Command {
	var yes, skipSpecs, noValidate, jsonMode bool
	cmd := &cobra.Command{
		Use:   "archive <change>",
		Short: "Archive a completed change and merge its deltas into the specs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRoot()
			if err != nil {
				return fail(jsonMode, map[string]any{"archive": nil}, "root_not_found", err)
			}
			ctx, err := change.LoadContext(root, args[0], "")
			if err != nil {
				return fail(jsonMode, map[string]any{"archive": nil}, "change_error", err)
			}

			opts := change.ArchiveOptions{SkipSpecs: skipSpecs, NoValidate: noValidate}
			if noValidate && !yes {
				return fail(jsonMode, map[string]any{"archive": nil}, "confirmation_required",
					fmt.Errorf("--no-validate requires --yes"))
			}
			if err := change.CheckArchiveReady(ctx, opts); err != nil {
				var incomplete *change.IncompleteTasksError
				if errors.As(err, &incomplete) {
					if !yes {
						if jsonMode {
							return fail(jsonMode, map[string]any{"archive": nil}, "incomplete_tasks",
								fmt.Errorf("%s; pass --yes to archive anyway", incomplete))
						}
						if !confirm(fmt.Sprintf("Change has %d/%d incomplete task(s). Archive anyway?", incomplete.Remaining, incomplete.Total)) {
							return fmt.Errorf("archive cancelled")
						}
					}
				} else {
					return fail(jsonMode, map[string]any{"archive": nil}, "validation_failed", err)
				}
			}

			result, err := change.Archive(ctx, opts)
			if err != nil {
				var conflict *merge.ConflictError
				if errors.As(err, &conflict) && jsonMode {
					payload := map[string]any{"archive": nil, "conflicts": conflict.Conflicts}
					return failJSON(payload, "fingerprint_conflict", err)
				}
				return fail(jsonMode, map[string]any{"archive": nil}, "archive_error", err)
			}

			if jsonMode {
				return printJSON(map[string]any{"archive": result})
			}
			for _, w := range result.Warnings {
				fmt.Printf("warning: %s\n", w)
			}
			for _, cap := range result.SpecsUpdated {
				fmt.Printf("Applied changes to openspec/specs/%s/spec.md:\n", cap.Capability)
				if cap.Added > 0 {
					fmt.Printf("  + %d added\n", cap.Added)
				}
				if cap.Modified > 0 {
					fmt.Printf("  ~ %d modified\n", cap.Modified)
				}
				if cap.Removed > 0 {
					fmt.Printf("  - %d removed\n", cap.Removed)
				}
				if cap.Renamed > 0 {
					fmt.Printf("  → %d renamed\n", cap.Renamed)
				}
			}
			fmt.Printf("Archived as %s\n", result.ArchivedAs)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompts")
	cmd.Flags().BoolVar(&skipSpecs, "skip-specs", false, "Archive without applying delta specs")
	cmd.Flags().BoolVar(&noValidate, "no-validate", false, "Skip pre-archive validation (requires --yes)")
	cmd.Flags().BoolVar(&jsonMode, "json", false, "Output as JSON")
	return cmd
}

func confirm(prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes"
}
