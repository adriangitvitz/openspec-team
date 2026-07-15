package change

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adriangitvitz/openspec-team/internal/core"
	"github.com/adriangitvitz/openspec-team/internal/fsutil"
	"github.com/adriangitvitz/openspec-team/internal/merge"
	"github.com/adriangitvitz/openspec-team/internal/parser"
	"github.com/adriangitvitz/openspec-team/internal/validate"
)

// ArchiveOptions configure Archive.
type ArchiveOptions struct {
	SkipSpecs  bool
	NoValidate bool

	Now func() time.Time
}

// CapabilityResult reports applied operations for one capability.
type CapabilityResult struct {
	Capability string `json:"capability"`
	merge.Counts
}

// ArchiveResult is the archive command payload.
type ArchiveResult struct {
	Change       string             `json:"change"`
	ArchivedAs   string             `json:"archivedAs"`
	Path         string             `json:"path"`
	SpecsUpdated []CapabilityResult `json:"specsUpdated"`
	Totals       merge.Counts       `json:"totals"`
	Warnings     []string           `json:"warnings,omitempty"`
}

// IncompleteTasksError signals unfinished tasks; callers may confirm and proceed.
type IncompleteTasksError struct {
	Remaining int
	Total     int
}

func (e *IncompleteTasksError) Error() string {
	return fmt.Sprintf("change has %d/%d incomplete task(s)", e.Remaining, e.Total)
}

// CheckArchiveReady runs the pre-archive validation and task-completion checks.
func CheckArchiveReady(ctx *Context, opts ArchiveOptions) error {
	if !opts.NoValidate && !opts.SkipSpecs {

		report := validate.ChangeDeltaSpecs(ctx.ChangeDir, false)
		if !report.Valid {
			var lines []string
			for _, issue := range report.Issues {
				if issue.Level == validate.Error {
					lines = append(lines, "  ✗ "+issue.Message)
				}
			}
			return fmt.Errorf("change %q failed validation:\n%s\nFix the issues or pass --no-validate to skip", ctx.ChangeName, strings.Join(lines, "\n"))
		}
	}

	progress := taskProgressForChange(ctx.Root, ctx.ChangeDir)
	if remaining := progress.Total - progress.Completed; remaining > 0 {
		return &IncompleteTasksError{Remaining: remaining, Total: progress.Total}
	}
	return nil
}

// Archive applies the change's deltas to the main specs (fingerprint-guarded,
// all-or-nothing) and moves the change into changes/archive/.
func Archive(ctx *Context, opts ArchiveOptions) (*ArchiveResult, error) {
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}

	result := &ArchiveResult{Change: ctx.ChangeName}

	if !opts.SkipSpecs {
		updates, err := merge.FindSpecUpdates(ctx.ChangeDir, core.SpecsDir(ctx.Root))
		if err != nil {
			return nil, err
		}

		type prepared struct {
			update  merge.SpecUpdate
			rebuilt string
			counts  merge.Counts
		}
		var all []prepared
		var conflicts []merge.Conflict
		anyUnchecked := false
		for _, update := range updates {
			built, err := merge.BuildUpdatedSpec(update, ctx.ChangeName)
			if err != nil {
				return nil, err
			}
			result.Warnings = append(result.Warnings, built.Warnings...)

			content, err := os.ReadFile(update.Source)
			if err != nil {
				return nil, err
			}
			cs, checked, err := merge.VerifyFingerprints(ctx.ChangeDir, update, parser.ParseDeltaSpec(string(content)))
			if err != nil {
				return nil, err
			}
			if !checked {
				anyUnchecked = true
			}
			conflicts = append(conflicts, cs...)
			all = append(all, prepared{update: update, rebuilt: built.Rebuilt, counts: built.Counts})
		}
		if len(conflicts) > 0 {
			return nil, &merge.ConflictError{ChangeName: ctx.ChangeName, Conflicts: conflicts}
		}
		if anyUnchecked {
			result.Warnings = append(result.Warnings,
				"no fingerprints recorded for this change (meta.json); parallel-edit conflict detection was skipped")
		}

		for _, p := range all {
			specName := filepath.Base(filepath.Dir(p.update.Target))
			report := validate.SpecContent(specName, p.rebuilt, false)
			if !report.Valid {
				var lines []string
				for _, issue := range report.Issues {
					if issue.Level == validate.Error {
						lines = append(lines, "  ✗ "+issue.Message)
					}
				}
				return nil, fmt.Errorf("validation errors in rebuilt spec for %s:\n%s", specName, strings.Join(lines, "\n"))
			}
		}

		for _, p := range all {
			if err := os.MkdirAll(filepath.Dir(p.update.Target), 0o755); err != nil {
				return nil, err
			}
			if err := fsutil.WriteFileAtomic(p.update.Target, []byte(p.rebuilt), 0o644); err != nil {
				return nil, err
			}
			capability := filepath.Base(filepath.Dir(p.update.Target))
			result.SpecsUpdated = append(result.SpecsUpdated, CapabilityResult{Capability: capability, Counts: p.counts})
			result.Totals.Added += p.counts.Added
			result.Totals.Modified += p.counts.Modified
			result.Totals.Removed += p.counts.Removed
			result.Totals.Renamed += p.counts.Renamed
		}
	}

	archivedAs := now().Format("2006-01-02") + "-" + ctx.ChangeName
	archivePath := filepath.Join(core.ArchiveDir(ctx.Root), archivedAs)
	if err := fsutil.MoveDir(ctx.ChangeDir, archivePath); err != nil {
		return nil, fmt.Errorf("specs were updated but the change could not be moved to the archive: %w", err)
	}
	result.ArchivedAs = archivedAs
	result.Path = archivePath
	return result, nil
}
