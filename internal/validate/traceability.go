package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/adriangitvitz/openspec-team/internal/parser"
	"github.com/adriangitvitz/openspec-team/internal/schema"
)

// Tasks reference requirements with inline `(req: <name>)` markers; names cannot
// contain `)`. Matching is case-sensitive and whitespace-normalized on both sides.
var (
	taskMarkerRe   = regexp.MustCompile(`\(req:\s*([^)]+)\)`)
	whitespaceRuns = regexp.MustCompile(`\s+`)
	checkboxRe     = regexp.MustCompile(`^\s*- \[[ xX]\]\s*(.*)$`)
	taskNumberRe   = regexp.MustCompile(`^\d+(\.\d+)*\.?\s+`)
)

func normalizeMarkerName(name string) string {
	return whitespaceRuns.ReplaceAllString(strings.TrimSpace(name), " ")
}

func normalizeTaskText(text string) string {
	text = taskMarkerRe.ReplaceAllString(text, "")
	text = taskNumberRe.ReplaceAllString(strings.TrimSpace(text), "")
	return normalizeMarkerName(text)
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

// TaskTraceability errors on markerless or duplicated checkbox task lines and
// warns on unreferenced requirements and on markers matching no requirement.
func TaskTraceability(changeDir, tasksFile string) []Issue {
	tasksContent, err := os.ReadFile(filepath.Join(changeDir, tasksFile))
	if err != nil {
		return nil
	}

	var issues []Issue
	duplicates := map[string][]int{}
	var duplicateOrder []string
	for i, line := range strings.Split(string(tasksContent), "\n") {
		m := checkboxRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		text := m[1]
		if !taskMarkerRe.MatchString(text) {
			issues = append(issues, Issue{
				Level: Error,
				Path:  "task-traceability",
				Message: fmt.Sprintf("task on line %d has no (req: <requirement name>) marker: %s",
					i+1, strings.TrimSpace(text)),
			})
		}
		norm := normalizeTaskText(text)
		if norm == "" {
			continue
		}
		if _, seen := duplicates[norm]; !seen {
			duplicateOrder = append(duplicateOrder, norm)
		}
		duplicates[norm] = append(duplicates[norm], i+1)
	}
	for _, norm := range duplicateOrder {
		lines := duplicates[norm]
		if len(lines) < 2 {
			continue
		}
		parts := make([]string, len(lines))
		for j, n := range lines {
			parts[j] = strconv.Itoa(n)
		}
		issues = append(issues, Issue{
			Level: Error,
			Path:  "task-traceability",
			Message: fmt.Sprintf("task text %q is duplicated on lines %s",
				norm, strings.Join(parts, ", ")),
		})
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
