// Package validate enforces the spec and change-delta rules; same rules,
// messages, and severities as the TS validator.
package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/adriangitvitz/openspec-team/internal/parser"
)

// Level is an issue severity.
type Level string

const (
	Error   Level = "ERROR"
	Warning Level = "WARNING"
	Info    Level = "INFO"
)

// Issue is a single validation finding.
type Issue struct {
	Level   Level  `json:"level"`
	Path    string `json:"path"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
}

// Summary tallies issues by level.
type Summary struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Info     int `json:"info"`
}

// Report is the result of validating one item.
type Report struct {
	Valid   bool    `json:"valid"`
	Issues  []Issue `json:"issues"`
	Summary Summary `json:"summary"`
}

const (
	MinPurposeLength         = 50
	MaxRequirementTextLength = 500
)

const (
	msgPurposeTooBrief        = "Purpose section is too brief (less than 50 characters)"
	msgRequirementTooLong     = "Requirement text is very long (>500 characters). Consider breaking it down."
	msgRequirementNoScenarios = "Requirement must have at least one scenario"
	msgRequirementEmpty       = "Requirement text cannot be empty"
	msgSpecNoRequirements     = "Spec must have at least one requirement"
	msgChangeNoDeltas         = "Change must have at least one delta"

	guideNoDeltas = `No deltas found. Ensure your change has a specs/ directory with capability folders (e.g. specs/http-server/spec.md) containing .md files that use delta headers (## ADDED/MODIFIED/REMOVED/RENAMED Requirements) and that each requirement includes at least one "#### Scenario:" block. Tip: inspect the delta files under the change's specs/ directory.`

	guideMissingSpecSections = "Missing required sections. Expected headers: \"## Purpose\" and \"## Requirements\". Example:\n## Purpose\n[brief purpose]\n\n## Requirements\n### Requirement: Clear requirement statement\nUsers SHALL ...\n\n#### Scenario: Descriptive name\n- **WHEN** ...\n- **THEN** ..."

	guideScenarioFormat = "Scenarios must use level-4 headers. Convert bullet lists into:\n#### Scenario: Short name\n- **WHEN** ...\n- **THEN** ...\n- **AND** ..."
)

func report(issues []Issue, strict bool) Report {
	var s Summary
	for _, i := range issues {
		switch i.Level {
		case Error:
			s.Errors++
		case Warning:
			s.Warnings++
		case Info:
			s.Info++
		}
	}
	valid := s.Errors == 0
	if strict {
		valid = valid && s.Warnings == 0
	}
	return Report{Valid: valid, Issues: issues, Summary: s}
}

// SpecFile validates a main spec file on disk.
func SpecFile(path string, strict bool) Report {
	content, err := os.ReadFile(path)
	if err != nil {
		return report([]Issue{{Level: Error, Path: "file", Message: err.Error()}}, strict)
	}
	return SpecContent(specNameFromPath(path), string(content), strict)
}

// SpecContent validates main-spec content.
func SpecContent(specName, content string, strict bool) Report {
	var issues []Issue

	spec, err := parser.ParseSpec(specName, content)
	if err != nil {
		return report([]Issue{{
			Level:   Error,
			Path:    "file",
			Message: fmt.Sprintf("%s. %s", capitalize(err.Error()), guideMissingSpecSections),
		}}, strict)
	}

	if len(spec.Requirements) == 0 {
		issues = append(issues, Issue{Level: Error, Path: "requirements", Message: msgSpecNoRequirements})
	}
	for i, req := range spec.Requirements {
		if req.Text == "" {
			issues = append(issues, Issue{Level: Error, Path: fmt.Sprintf("requirements.%d.text", i), Message: msgRequirementEmpty})
		}
		if len(req.Scenarios) == 0 {
			issues = append(issues, Issue{Level: Error, Path: fmt.Sprintf("requirements.%d.scenarios", i), Message: msgRequirementNoScenarios})
		}
	}

	for _, si := range parser.FindMainSpecStructureIssues(content) {
		issues = append(issues, Issue{Level: Error, Path: "file", Line: si.Line, Message: si.Message})
	}

	if len(spec.Purpose) < MinPurposeLength {
		issues = append(issues, Issue{Level: Warning, Path: "overview", Message: msgPurposeTooBrief})
	}
	for i, req := range spec.Requirements {
		if len(req.Text) > MaxRequirementTextLength {
			issues = append(issues, Issue{Level: Info, Path: fmt.Sprintf("requirements[%d]", i), Message: msgRequirementTooLong})
		}
		if len(req.Scenarios) == 0 {
			issues = append(issues, Issue{
				Level:   Warning,
				Path:    fmt.Sprintf("requirements[%d].scenarios", i),
				Message: fmt.Sprintf("%s. %s", msgRequirementNoScenarios, guideScenarioFormat),
			})
		}
	}

	for i, block := range parser.ExtractRequirementsSection(content).Blocks {
		text := requirementBodyText(block.Raw)
		if text == "" || !parser.ContainsShallOrMust(text) {
			issues = append(issues, Issue{
				Level:   Error,
				Path:    fmt.Sprintf("requirements[%d]", i),
				Message: missingShallOrMustMessage(fmt.Sprintf("Requirement %q", block.Name), block.Name),
			})
		}
	}

	return report(issues, strict)
}

// ChangeDeltaSpecs validates every delta spec.md under a change directory.
func ChangeDeltaSpecs(changeDir string, strict bool) Report {
	var issues []Issue
	specsDir := filepath.Join(changeDir, "specs")
	totalDeltas := 0
	var missingHeaderSpecs []string
	type emptySection struct {
		path     string
		sections []string
	}
	var emptySectionSpecs []emptySection

	for _, specFile := range findDeltaSpecFiles(specsDir) {
		content, err := os.ReadFile(specFile)
		if err != nil {
			continue
		}
		plan := parser.ParseDeltaSpec(string(content))
		rel, _ := filepath.Rel(specsDir, specFile)
		entryPath := filepath.ToSlash(rel)

		for _, stray := range plan.SkippedHeaders {
			nameless := regexp.MustCompile(`(?i)^requirement:?$`).MatchString(stray.Header)
			var msg string
			if nameless {
				msg = fmt.Sprintf("Header \"### %s\" in %s is missing a requirement name and is ignored by validation. Add a name, e.g. \"### Requirement: <name>\".", stray.Header, stray.Section)
			} else {
				msg = fmt.Sprintf("Header \"### %s\" in %s is not a \"### Requirement:\" header and is ignored by validation. Use \"### Requirement: %s\" if it should be validated as a requirement.", stray.Header, stray.Section, stray.Header)
			}
			issues = append(issues, Issue{Level: Info, Path: entryPath, Line: stray.Line, Message: msg})
		}

		var sectionNames []string
		if plan.SectionPresence.Added {
			sectionNames = append(sectionNames, "## ADDED Requirements")
		}
		if plan.SectionPresence.Modified {
			sectionNames = append(sectionNames, "## MODIFIED Requirements")
		}
		if plan.SectionPresence.Removed {
			sectionNames = append(sectionNames, "## REMOVED Requirements")
		}
		if plan.SectionPresence.Renamed {
			sectionNames = append(sectionNames, "## RENAMED Requirements")
		}
		hasEntries := len(plan.Added)+len(plan.Modified)+len(plan.Removed)+len(plan.Renamed) > 0
		if !hasEntries {
			if len(sectionNames) > 0 {
				emptySectionSpecs = append(emptySectionSpecs, emptySection{path: entryPath, sections: sectionNames})
			} else {
				missingHeaderSpecs = append(missingHeaderSpecs, entryPath)
			}
		}

		addedNames := map[string]bool{}
		modifiedNames := map[string]bool{}
		removedNames := map[string]bool{}
		renamedFrom := map[string]bool{}
		renamedTo := map[string]bool{}

		validateBlock := func(op string, block parser.RequirementBlock, seen map[string]bool) {
			key := parser.NormalizeRequirementName(block.Name)
			totalDeltas++
			if seen[key] {
				issues = append(issues, Issue{Level: Error, Path: entryPath, Message: fmt.Sprintf("Duplicate requirement in %s: %q", op, block.Name)})
			} else {
				seen[key] = true
			}
			prefix := fmt.Sprintf("%s %q", op, block.Name)
			text := requirementBodyText(block.Raw)
			if text == "" {
				if parser.ContainsShallOrMust(block.Name) {
					issues = append(issues, Issue{Level: Error, Path: entryPath, Message: missingShallOrMustMessage(prefix, block.Name)})
				} else {
					issues = append(issues, Issue{Level: Error, Path: entryPath, Message: prefix + " is missing requirement text"})
				}
			} else if !parser.ContainsShallOrMust(text) {
				issues = append(issues, Issue{Level: Error, Path: entryPath, Message: missingShallOrMustMessage(prefix, block.Name)})
			}
			if countBlockScenarios(block.Raw) < 1 {
				issues = append(issues, Issue{Level: Error, Path: entryPath, Message: prefix + " must include at least one scenario"})
			}
		}
		for _, block := range plan.Added {
			validateBlock("ADDED", block, addedNames)
		}
		for _, block := range plan.Modified {
			validateBlock("MODIFIED", block, modifiedNames)
		}
		for _, name := range plan.Removed {
			key := parser.NormalizeRequirementName(name)
			totalDeltas++
			if removedNames[key] {
				issues = append(issues, Issue{Level: Error, Path: entryPath, Message: fmt.Sprintf("Duplicate requirement in REMOVED: %q", name)})
			} else {
				removedNames[key] = true
			}
		}
		for _, ren := range plan.Renamed {
			fromKey := parser.NormalizeRequirementName(ren.From)
			toKey := parser.NormalizeRequirementName(ren.To)
			totalDeltas++
			if renamedFrom[fromKey] {
				issues = append(issues, Issue{Level: Error, Path: entryPath, Message: fmt.Sprintf("Duplicate FROM in RENAMED: %q", ren.From)})
			} else {
				renamedFrom[fromKey] = true
			}
			if renamedTo[toKey] {
				issues = append(issues, Issue{Level: Error, Path: entryPath, Message: fmt.Sprintf("Duplicate TO in RENAMED: %q", ren.To)})
			} else {
				renamedTo[toKey] = true
			}
		}

		for _, n := range sortedKeys(modifiedNames) {
			if removedNames[n] {
				issues = append(issues, Issue{Level: Error, Path: entryPath, Message: fmt.Sprintf("Requirement present in both MODIFIED and REMOVED: %q", n)})
			}
			if addedNames[n] {
				issues = append(issues, Issue{Level: Error, Path: entryPath, Message: fmt.Sprintf("Requirement present in both MODIFIED and ADDED: %q", n)})
			}
		}
		for _, n := range sortedKeys(addedNames) {
			if removedNames[n] {
				issues = append(issues, Issue{Level: Error, Path: entryPath, Message: fmt.Sprintf("Requirement present in both ADDED and REMOVED: %q", n)})
			}
		}
		for _, ren := range plan.Renamed {
			if modifiedNames[parser.NormalizeRequirementName(ren.From)] {
				issues = append(issues, Issue{Level: Error, Path: entryPath, Message: fmt.Sprintf("MODIFIED references old name from RENAMED. Use new header for %q", ren.To)})
			}
			if addedNames[parser.NormalizeRequirementName(ren.To)] {
				issues = append(issues, Issue{Level: Error, Path: entryPath, Message: fmt.Sprintf("RENAMED TO collides with ADDED for %q", ren.To)})
			}
		}
	}

	for _, es := range emptySectionSpecs {
		issues = append(issues, Issue{
			Level: Error,
			Path:  es.path,
			Message: fmt.Sprintf(
				"Delta sections %s were found, but no requirement entries parsed. Ensure each section includes at least one \"### Requirement:\" block (REMOVED may use bullet list syntax).",
				formatSectionList(es.sections)),
		})
	}
	for _, p := range missingHeaderSpecs {
		issues = append(issues, Issue{
			Level:   Error,
			Path:    p,
			Message: `No delta sections found. Add headers such as "## ADDED Requirements" or move non-delta notes outside specs/.`,
		})
	}

	if totalDeltas == 0 {
		issues = append(issues, Issue{Level: Error, Path: "file", Message: fmt.Sprintf("%s. %s", msgChangeNoDeltas, guideNoDeltas)})
	}

	if _, err := os.Stat(filepath.Join(changeDir, "proposal.md")); err != nil {
		issues = append(issues, Issue{Level: Warning, Path: "proposal.md", Message: "proposal.md is missing"})
	}

	return report(issues, strict)
}

func findDeltaSpecFiles(specsDir string) []string {
	var results []string
	filepath.WalkDir(specsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && d.Name() == "spec.md" {
			results = append(results, path)
		}
		return nil
	})
	sort.Strings(results)
	return results
}

// requirementBodyText skips the header line so header-only SHALL/MUST still gets the body hint.
func requirementBodyText(blockRaw string) string {
	lines := strings.Split(blockRaw, "\n")
	if len(lines) <= 1 {
		return ""
	}
	return parser.ExtractRequirementBody(lines[1:])
}

func countBlockScenarios(blockRaw string) int {
	lines := strings.Split(blockRaw, "\n")
	if len(lines) <= 1 {
		return 0
	}
	return parser.CountScenarios(lines[1:])
}

func missingShallOrMustMessage(prefix, blockName string) string {
	base := prefix + " must contain SHALL or MUST"
	if parser.ContainsShallOrMust(blockName) {
		return base + ` in the requirement body, not only in the header. Move the SHALL/MUST statement to the line immediately after the "### Requirement: ..." header.`
	}
	return base
}

func formatSectionList(sections []string) string {
	switch len(sections) {
	case 0:
		return ""
	case 1:
		return sections[0]
	default:
		return strings.Join(sections[:len(sections)-1], ", ") + " and " + sections[len(sections)-1]
	}
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// specNameFromPath returns the directory after "specs" or "changes", else the file stem.
func specNameFromPath(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if (parts[i] == "specs" || parts[i] == "changes") && i < len(parts)-1 {
			return parts[i+1]
		}
	}
	name := parts[len(parts)-1]
	if dot := strings.LastIndex(name, "."); dot > 0 {
		return name[:dot]
	}
	return name
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
