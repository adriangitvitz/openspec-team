package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/adriangitvitz/openspec-go/internal/change"
	"github.com/adriangitvitz/openspec-go/internal/core"
	"github.com/adriangitvitz/openspec-go/internal/validate"
)

type validationResult struct {
	Item   string          `json:"item"`
	Type   string          `json:"type"`
	Report validate.Report `json:"report"`
}

func newValidateCmd() *cobra.Command {
	var all, changes, specs, strict, jsonMode, refreshFingerprints bool
	cmd := &cobra.Command{
		Use:   "validate [item]",
		Short: "Validate changes and specs",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRoot()
			if err != nil {
				return fail(jsonMode, nil, "root_not_found", err)
			}

			var results []validationResult

			validateChange := func(name string) error {
				ctx, err := change.LoadContext(root, name, "")
				if err != nil {
					return err
				}
				if refreshFingerprints {
					refreshChangeFingerprints(ctx)
				} else {
					captureFingerprints(ctx)
				}
				report := validate.ChangeDeltaSpecs(ctx.ChangeDir, strict)
				if coverage := validate.KnowledgeCoverage(ctx.ChangeDir, core.ReadProjectConfig(root).Knowledge); len(coverage) > 0 {
					report = validate.NewReport(append(report.Issues, coverage...), strict)
				}

				if tr := validate.TaskTraceabilityForSchema(ctx.Schema, ctx.ChangeDir); len(tr) > 0 {
					report = validate.NewReport(append(report.Issues, tr...), strict)
				}
				results = append(results, validationResult{
					Item:   name,
					Type:   "change",
					Report: report,
				})
				return nil
			}
			validateSpec := func(name string) {
				path := filepath.Join(core.SpecsDir(root), name, "spec.md")
				results = append(results, validationResult{
					Item:   name,
					Type:   "spec",
					Report: validate.SpecFile(path, strict),
				})
			}

			switch {
			case len(args) == 1:
				name := args[0]
				if _, err := os.Stat(filepath.Join(core.ChangesDir(root), name)); err == nil {
					if err := validateChange(name); err != nil {
						return fail(jsonMode, nil, "change_error", err)
					}
				} else if _, err := os.Stat(filepath.Join(core.SpecsDir(root), name, "spec.md")); err == nil {
					validateSpec(name)
				} else {
					return fail(jsonMode, nil, "item_not_found",
						fmt.Errorf("no change or spec named %q found", name))
				}
			case all || (changes && specs):
				for _, name := range change.ActiveChangeNames(root) {
					if err := validateChange(name); err != nil {
						return fail(jsonMode, nil, "change_error", err)
					}
				}
				for _, s := range change.ListSpecs(root) {
					validateSpec(s.ID)
				}
			case specs:
				for _, s := range change.ListSpecs(root) {
					validateSpec(s.ID)
				}
			case changes:
				fallthrough
			default:
				for _, name := range change.ActiveChangeNames(root) {
					if err := validateChange(name); err != nil {
						return fail(jsonMode, nil, "change_error", err)
					}
				}
			}

			if len(args) == 0 {
				if report, hasKnowledge := knowledgeReport(root, strict); hasKnowledge {
					results = append(results, validationResult{Item: "knowledge-map", Type: "config", Report: report})
				}
				if report, hasTeam := teamConfigReport(root, strict); hasTeam {
					results = append(results, validationResult{Item: "team-config", Type: "config", Report: report})
				}
			}

			failed := 0
			for _, r := range results {
				if !r.Report.Valid {
					failed++
				}
			}

			if jsonMode {
				if results == nil {
					results = []validationResult{}
				}
				if err := printJSON(map[string]any{"results": results, "valid": failed == 0}); err != nil {
					return err
				}
			} else {
				printValidationText(results)
			}
			if failed > 0 {
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Validate all changes and specs")
	cmd.Flags().BoolVar(&changes, "changes", false, "Validate all changes")
	cmd.Flags().BoolVar(&specs, "specs", false, "Validate all specs")
	cmd.Flags().BoolVar(&strict, "strict", false, "Treat warnings as failures")
	cmd.Flags().BoolVar(&jsonMode, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&refreshFingerprints, "refresh-fingerprints", false,
		"Re-baseline this change's requirement fingerprints against the current specs")
	return cmd
}

func knowledgeReport(root string, strict bool) (validate.Report, bool) {
	cfg := core.ReadProjectConfig(root)
	if len(cfg.Knowledge) == 0 {
		return validate.Report{}, false
	}
	var issues []validate.Issue
	for _, entry := range cfg.Knowledge {
		if len(entry.Paths) == 0 {
			issues = append(issues, validate.Issue{
				Level:   validate.Warning,
				Path:    "knowledge",
				Message: fmt.Sprintf("knowledge topic %q has no paths", entry.Topic),
			})
		}
		for _, p := range entry.Paths {
			full := p
			if !filepath.IsAbs(full) {
				full = filepath.Join(root, p)
			}
			if _, err := os.Stat(full); err != nil {
				issues = append(issues, validate.Issue{
					Level:   validate.Warning,
					Path:    "knowledge",
					Message: fmt.Sprintf("knowledge topic %q: path does not exist: %s", entry.Topic, p),
				})
			}
		}
	}
	report := validate.Report{Valid: true, Issues: issues}
	report.Summary.Warnings = len(issues)
	if strict && len(issues) > 0 {
		report.Valid = false
	}
	if report.Issues == nil {
		report.Issues = []validate.Issue{}
	}
	return report, true
}

func teamConfigReport(root string, strict bool) (validate.Report, bool) {
	return validate.TeamConfigReport(root, core.ReadProjectConfig(root).Team, strict)
}

func printValidationText(results []validationResult) {
	if len(results) == 0 {
		fmt.Println("Nothing to validate.")
		return
	}
	for _, r := range results {
		mark := "✓"
		if !r.Report.Valid {
			mark = "✗"
		}
		fmt.Printf("%s %s (%s)\n", mark, r.Item, r.Type)
		for _, issue := range r.Report.Issues {
			loc := issue.Path
			if issue.Line > 0 {
				loc = fmt.Sprintf("%s:%d", issue.Path, issue.Line)
			}
			fmt.Printf("  [%s] %s — %s\n", issue.Level, loc, issue.Message)
		}
	}
}
