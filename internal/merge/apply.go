// Package merge applies a change's delta specs to the main specs, guarded
// by requirement fingerprints.
package merge

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/adriangitvitz/openspec-team/internal/parser"
)

// SpecUpdate pairs a change's delta spec file with its main-spec target.
type SpecUpdate struct {
	Source string
	Target string
	Exists bool
}

// Counts tallies applied operations for one capability.
type Counts struct {
	Added    int `json:"added"`
	Modified int `json:"modified"`
	Removed  int `json:"removed"`
	Renamed  int `json:"renamed"`
}

// BuildResult is a rebuilt spec ready to write.
type BuildResult struct {
	Rebuilt  string
	Counts   Counts
	Warnings []string
}

// FindSpecUpdates pairs each specs/<capability>/spec.md delta with its main-spec target.
func FindSpecUpdates(changeDir, mainSpecsDir string) ([]SpecUpdate, error) {
	entries, err := os.ReadDir(filepath.Join(changeDir, "specs"))
	if err != nil {
		return nil, nil
	}
	var updates []SpecUpdate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		source := filepath.Join(changeDir, "specs", entry.Name(), "spec.md")
		if _, err := os.Stat(source); err != nil {
			continue
		}
		target := filepath.Join(mainSpecsDir, entry.Name(), "spec.md")
		_, err := os.Stat(target)
		updates = append(updates, SpecUpdate{Source: source, Target: target, Exists: err == nil})
	}
	return updates, nil
}

var modifiedHeaderRe = regexp.MustCompile(`(?i)^###\s*Requirement:\s*(.+)\s*$`)

// BuildUpdatedSpec applies one delta file to its target spec in memory;
// fingerprint conflict detection runs separately.
func BuildUpdatedSpec(update SpecUpdate, changeName string) (*BuildResult, error) {
	changeContent, err := os.ReadFile(update.Source)
	if err != nil {
		return nil, err
	}
	plan := parser.ParseDeltaSpec(string(changeContent))
	specName := filepath.Base(filepath.Dir(update.Target))
	var warnings []string

	if err := checkDuplicates(specName, plan); err != nil {
		return nil, err
	}
	if err := checkCrossSection(specName, plan); err != nil {
		return nil, err
	}
	if len(plan.Added)+len(plan.Modified)+len(plan.Removed)+len(plan.Renamed) == 0 {
		return nil, fmt.Errorf(
			"delta parsing found no operations for %s. Provide ADDED/MODIFIED/REMOVED/RENAMED sections in change spec",
			filepath.Base(filepath.Dir(update.Source)))
	}

	isNewSpec := false
	targetBytes, err := os.ReadFile(update.Target)
	targetContent := string(targetBytes)
	if err != nil {
		if len(plan.Modified) > 0 || len(plan.Renamed) > 0 {
			return nil, fmt.Errorf(
				"%s: target spec does not exist; only ADDED requirements are allowed for new specs. MODIFIED and RENAMED operations require an existing spec",
				specName)
		}
		if len(plan.Removed) > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"%s - %d REMOVED requirement(s) ignored for new spec (nothing to remove)",
				specName, len(plan.Removed)))
		}
		isNewSpec = true
		targetContent = BuildSpecSkeleton(specName, changeName)
	}

	if issues := parser.FindMainSpecStructureIssues(targetContent); len(issues) > 0 {
		var details []string
		for _, issue := range issues {
			details = append(details, fmt.Sprintf("line %d: %s", issue.Line, issue.Message))
		}
		return nil, fmt.Errorf(
			"%s: target spec is structurally invalid and cannot be updated until fixed:\n%s",
			specName, strings.Join(details, "\n"))
	}

	parts := parser.ExtractRequirementsSection(targetContent)
	nameToBlock := map[string]parser.RequirementBlock{}
	for _, block := range parts.Blocks {
		nameToBlock[parser.NormalizeRequirementName(block.Name)] = block
	}

	for _, r := range plan.Renamed {
		from := parser.NormalizeRequirementName(r.From)
		to := parser.NormalizeRequirementName(r.To)
		block, ok := nameToBlock[from]
		if !ok {
			return nil, fmt.Errorf("%s RENAMED failed for header \"### Requirement: %s\" - source not found", specName, r.From)
		}
		if _, exists := nameToBlock[to]; exists {
			return nil, fmt.Errorf("%s RENAMED failed for header \"### Requirement: %s\" - target already exists", specName, r.To)
		}
		newHeader := "### Requirement: " + to
		rawLines := strings.Split(block.Raw, "\n")
		rawLines[0] = newHeader
		delete(nameToBlock, from)
		nameToBlock[to] = parser.RequirementBlock{
			HeaderLine: newHeader,
			Name:       to,
			Raw:        strings.Join(rawLines, "\n"),
		}
	}

	for _, name := range plan.Removed {
		key := parser.NormalizeRequirementName(name)
		if _, ok := nameToBlock[key]; !ok {
			if !isNewSpec {
				return nil, fmt.Errorf("%s REMOVED failed for header \"### Requirement: %s\" - not found", specName, name)
			}
			continue
		}
		delete(nameToBlock, key)
	}

	for _, mod := range plan.Modified {
		key := parser.NormalizeRequirementName(mod.Name)
		currentBlock, ok := nameToBlock[key]
		if !ok {
			return nil, fmt.Errorf("%s MODIFIED failed for header \"### Requirement: %s\" - not found", specName, mod.Name)
		}
		headerMatch := modifiedHeaderRe.FindStringSubmatch(strings.Split(mod.Raw, "\n")[0])
		if headerMatch == nil || parser.NormalizeRequirementName(headerMatch[1]) != key {
			return nil, fmt.Errorf("%s MODIFIED failed for header \"### Requirement: %s\" - header mismatch in content", specName, mod.Name)
		}

		if missing := findMissingCurrentScenarios(currentBlock, mod); len(missing) > 0 {
			return nil, fmt.Errorf(
				"%s MODIFIED failed for header \"### Requirement: %s\" - current spec contains scenario(s) not present in the modified block: %s. Refresh the change spec before archiving to avoid dropping scenarios",
				specName, mod.Name, quoteList(missing))
		}
		nameToBlock[key] = mod
	}

	for _, add := range plan.Added {
		key := parser.NormalizeRequirementName(add.Name)
		if _, exists := nameToBlock[key]; exists {
			return nil, fmt.Errorf("%s ADDED failed for header \"### Requirement: %s\" - already exists", specName, add.Name)
		}
		nameToBlock[key] = add
	}

	var keptOrder []parser.RequirementBlock
	seen := map[string]bool{}
	for _, block := range parts.Blocks {
		key := parser.NormalizeRequirementName(block.Name)
		if replacement, ok := nameToBlock[key]; ok {
			keptOrder = append(keptOrder, replacement)
			seen[key] = true
		}
	}
	for _, r := range plan.Renamed {
		key := parser.NormalizeRequirementName(r.To)
		if !seen[key] {
			keptOrder = append(keptOrder, nameToBlock[key])
			seen[key] = true
		}
	}
	for _, add := range plan.Added {
		key := parser.NormalizeRequirementName(add.Name)
		if !seen[key] {
			keptOrder = append(keptOrder, nameToBlock[key])
			seen[key] = true
		}
	}

	var bodyParts []string
	if strings.TrimSpace(parts.Preamble) != "" {
		bodyParts = append(bodyParts, strings.TrimRight(parts.Preamble, " \t\n"))
	}
	for _, block := range keptOrder {
		bodyParts = append(bodyParts, block.Raw)
	}
	reqBody := strings.TrimRight(strings.Join(bodyParts, "\n\n"), " \t\n")

	var docParts []string
	if before := strings.TrimRight(parts.Before, " \t\n"); before != "" {
		docParts = append(docParts, before)
	}
	docParts = append(docParts, parts.HeaderLine, reqBody, parts.After)
	rebuilt := regexp.MustCompile(`\n{3,}`).ReplaceAllString(strings.Join(docParts, "\n"), "\n\n")

	return &BuildResult{
		Rebuilt: rebuilt,
		Counts: Counts{
			Added:    len(plan.Added),
			Modified: len(plan.Modified),
			Removed:  len(plan.Removed),
			Renamed:  len(plan.Renamed),
		},
		Warnings: warnings,
	}, nil
}

// BuildSpecSkeleton is the starting content for a spec created by archiving.
func BuildSpecSkeleton(specFolderName, changeName string) string {
	return fmt.Sprintf(
		"# %s Specification\n\n## Purpose\nTBD - created by archiving change %s. Update Purpose after archive.\n\n## Requirements\n",
		specFolderName, changeName)
}

func checkDuplicates(specName string, plan parser.DeltaPlan) error {
	dup := func(section string, name string) error {
		return fmt.Errorf("%s validation failed - duplicate requirement in %s for header \"### Requirement: %s\"", specName, section, name)
	}
	seen := map[string]bool{}
	for _, b := range plan.Added {
		key := parser.NormalizeRequirementName(b.Name)
		if seen[key] {
			return dup("ADDED", b.Name)
		}
		seen[key] = true
	}
	seen = map[string]bool{}
	for _, b := range plan.Modified {
		key := parser.NormalizeRequirementName(b.Name)
		if seen[key] {
			return dup("MODIFIED", b.Name)
		}
		seen[key] = true
	}
	seen = map[string]bool{}
	for _, name := range plan.Removed {
		key := parser.NormalizeRequirementName(name)
		if seen[key] {
			return dup("REMOVED", name)
		}
		seen[key] = true
	}
	from := map[string]bool{}
	to := map[string]bool{}
	for _, r := range plan.Renamed {
		fromKey := parser.NormalizeRequirementName(r.From)
		toKey := parser.NormalizeRequirementName(r.To)
		if from[fromKey] {
			return fmt.Errorf("%s validation failed - duplicate FROM in RENAMED for header \"### Requirement: %s\"", specName, r.From)
		}
		if to[toKey] {
			return fmt.Errorf("%s validation failed - duplicate TO in RENAMED for header \"### Requirement: %s\"", specName, r.To)
		}
		from[fromKey] = true
		to[toKey] = true
	}
	return nil
}

func checkCrossSection(specName string, plan parser.DeltaPlan) error {
	added := nameSet(blockNames(plan.Added))
	modified := nameSet(blockNames(plan.Modified))
	removed := nameSet(plan.Removed)

	for _, r := range plan.Renamed {
		if modified[parser.NormalizeRequirementName(r.From)] {
			return fmt.Errorf("%s validation failed - when a rename exists, MODIFIED must reference the NEW header \"### Requirement: %s\"", specName, r.To)
		}
		if added[parser.NormalizeRequirementName(r.To)] {
			return fmt.Errorf("%s validation failed - RENAMED TO header collides with ADDED for \"### Requirement: %s\"", specName, r.To)
		}
	}
	for name := range modified {
		if removed[name] {
			return crossSectionErr(specName, "MODIFIED", "REMOVED", name)
		}
		if added[name] {
			return crossSectionErr(specName, "MODIFIED", "ADDED", name)
		}
	}
	for name := range added {
		if removed[name] {
			return crossSectionErr(specName, "ADDED", "REMOVED", name)
		}
	}
	return nil
}

func crossSectionErr(specName, a, b, name string) error {
	return fmt.Errorf("%s validation failed - requirement present in multiple sections (%s and %s) for header \"### Requirement: %s\"", specName, a, b, name)
}

func blockNames(blocks []parser.RequirementBlock) []string {
	names := make([]string, len(blocks))
	for i, b := range blocks {
		names[i] = b.Name
	}
	return names
}

func nameSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[parser.NormalizeRequirementName(n)] = true
	}
	return set
}

var scenarioHeaderNamedRe = regexp.MustCompile(`^####\s*Scenario:\s*(.+)\s*$`)

func findMissingCurrentScenarios(current, incoming parser.RequirementBlock) []string {
	incomingNames := map[string]bool{}
	for _, s := range parseScenarioBlocks(incoming.Raw) {
		incomingNames[s] = true
	}
	var missing []string
	for _, s := range parseScenarioBlocks(current.Raw) {
		if !incomingNames[s] {
			missing = append(missing, s)
		}
	}
	return missing
}

func parseScenarioBlocks(requirementRaw string) []string {
	var names []string
	for _, line := range strings.Split(parser.NormalizeLineEndings(requirementRaw), "\n") {
		if m := scenarioHeaderNamedRe.FindStringSubmatch(line); m != nil {
			names = append(names, strings.TrimSpace(m[1]))
		}
	}
	return names
}

func quoteList(items []string) string {
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = fmt.Sprintf("%q", item)
	}
	return strings.Join(quoted, ", ")
}
