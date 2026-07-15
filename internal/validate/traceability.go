package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/adriangitvitz/openspec-go/internal/parser"
	"github.com/adriangitvitz/openspec-go/internal/schema"
)

// Tasks reference requirements with inline `(req: <name>)` markers; names cannot
// contain `)`. Matching is case-sensitive and whitespace-normalized on both sides.
var (
	taskMarkerRe   = regexp.MustCompile(`\(req:\s*([^)]+)\)`)
	whitespaceRuns = regexp.MustCompile(`\s+`)
)

func normalizeMarkerName(name string) string {
	return whitespaceRuns.ReplaceAllString(strings.TrimSpace(name), " ")
}

// TaskTraceabilityForSchema runs the traceability rule only for schemas that opt in via apply.traceability.
func TaskTraceabilityForSchema(s *schema.Schema, changeDir string) []Issue {
	if s == nil || s.Apply == nil || !s.Apply.Traceability {
		return nil
	}
	tracked := "tasks.md"
	if a, ok := s.TrackedTasksArtifact(); ok {
		tracked = a.Generates
	}
	return TaskTraceability(changeDir, tracked)
}

// TaskTraceability warns on delta requirements no task references and on markers matching no requirement.
func TaskTraceability(changeDir, tasksFile string) []Issue {
	tasksContent, err := os.ReadFile(filepath.Join(changeDir, tasksFile))
	if err != nil {
		return nil
	}

	referenced := map[string]bool{}
	var markerOrder []string
	for _, m := range taskMarkerRe.FindAllStringSubmatch(string(tasksContent), -1) {
		name := normalizeMarkerName(m[1])
		if !referenced[name] {
			markerOrder = append(markerOrder, name)
		}
		referenced[name] = true
	}

	type requirement struct{ name, capability string }
	var requirements []requirement
	defined := map[string]bool{}
	specsDir := filepath.Join(changeDir, "specs")
	for _, specFile := range findDeltaSpecFiles(specsDir) {
		content, err := os.ReadFile(specFile)
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(specsDir, specFile)
		capability := filepath.ToSlash(filepath.Dir(rel))
		plan := parser.ParseDeltaSpec(string(content))
		for _, block := range append(plan.Added, plan.Modified...) {
			name := normalizeMarkerName(block.Name)
			requirements = append(requirements, requirement{name: name, capability: capability})
			defined[name] = true
		}
		for _, name := range plan.Removed {
			defined[normalizeMarkerName(name)] = true
		}
		for _, ren := range plan.Renamed {
			defined[normalizeMarkerName(ren.From)] = true
			defined[normalizeMarkerName(ren.To)] = true
		}
	}

	var issues []Issue
	for _, req := range requirements {
		if !referenced[req.name] {
			issues = append(issues, Issue{
				Level: Warning,
				Path:  "task-traceability",
				Message: fmt.Sprintf("requirement %q (%s) has no task referencing it; add a task with the marker (req: %s)",
					req.name, req.capability, req.name),
			})
		}
	}
	for _, name := range markerOrder {
		if !defined[name] {
			issues = append(issues, Issue{
				Level: Warning,
				Path:  "task-traceability",
				Message: fmt.Sprintf("task marker (req: %s) matches no requirement in this change's delta specs",
					name),
			})
		}
	}
	return issues
}
