package change

import (
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/adriangitvitz/openspec-go/internal/core"
	"github.com/adriangitvitz/openspec-go/internal/parser"
	"github.com/adriangitvitz/openspec-go/internal/schema"
)

// ChangeSummary is one row of `openspec list --json`.
type ChangeSummary struct {
	Name           string    `json:"name"`
	CompletedTasks int       `json:"completedTasks"`
	TotalTasks     int       `json:"totalTasks"`
	LastModified   time.Time `json:"lastModified"`
	Status         string    `json:"status"`
}

// ListChanges summarizes active changes with task progress.
func ListChanges(root, sortBy string) []ChangeSummary {
	var changes []ChangeSummary
	for _, name := range ActiveChangeNames(root) {
		changeDir := filepath.Join(core.ChangesDir(root), name)
		progress := taskProgressForChange(root, changeDir)
		status := "in-progress"
		switch {
		case progress.Total == 0:
			status = "no-tasks"
		case progress.Completed == progress.Total:
			status = "complete"
		}
		changes = append(changes, ChangeSummary{
			Name:           name,
			CompletedTasks: progress.Completed,
			TotalTasks:     progress.Total,
			LastModified:   lastModified(changeDir),
			Status:         status,
		})
	}
	if sortBy == "name" {
		sort.Slice(changes, func(i, j int) bool { return changes[i].Name < changes[j].Name })
	} else {
		sort.Slice(changes, func(i, j int) bool { return changes[i].LastModified.After(changes[j].LastModified) })
	}
	return changes
}

// SpecSummary is one row of `openspec list --specs --json`.
type SpecSummary struct {
	ID               string `json:"id"`
	RequirementCount int    `json:"requirementCount"`
}

// ListSpecs summarizes the main specs.
func ListSpecs(root string) []SpecSummary {
	entries, err := os.ReadDir(core.SpecsDir(root))
	if err != nil {
		return nil
	}
	var specs []SpecSummary
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(core.SpecsDir(root), e.Name(), "spec.md"))
		if err != nil {
			continue
		}
		specs = append(specs, SpecSummary{
			ID:               e.Name(),
			RequirementCount: len(parser.ExtractRequirementsSection(string(content)).Blocks),
		})
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].ID < specs[j].ID })
	return specs
}

// taskProgressForChange counts checkboxes in tracked-tasks outputs, falling back to a top-level tasks.md.
func taskProgressForChange(root, changeDir string) parser.TaskProgress {
	schemaName := core.ResolveSchemaForChange(changeDir, "", root)
	if s, err := schema.Resolve(schemaName, root); err == nil {
		if a, ok := s.TrackedTasksArtifact(); ok {
			files := schema.ResolveArtifactOutputs(changeDir, a.Generates)
			if len(files) > 0 {
				var total parser.TaskProgress
				for _, f := range files {
					if content, err := os.ReadFile(f); err == nil {
						p := parser.CountTasks(string(content))
						total.Total += p.Total
						total.Completed += p.Completed
					}
				}
				return total
			}
		}
	}
	if content, err := os.ReadFile(filepath.Join(changeDir, "tasks.md")); err == nil {
		return parser.CountTasks(string(content))
	}
	return parser.TaskProgress{}
}

func lastModified(dir string) time.Time {
	newest := time.Time{}
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return newest
}
