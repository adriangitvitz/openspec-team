package parser

import (
	"regexp"
	"strings"
)

// SkippedHeader is a non-canonical level-3 header inside a delta section.
type SkippedHeader struct {
	Header  string
	Section string
	Line    int
}

// Rename is a RENAMED Requirements FROM/TO pair.
type Rename struct {
	From string
	To   string
}

// SectionPresence records which delta sections appeared, even when empty.
type SectionPresence struct {
	Added    bool
	Modified bool
	Removed  bool
	Renamed  bool
}

// DeltaPlan is the parsed content of a delta-formatted spec change file.
type DeltaPlan struct {
	Added           []RequirementBlock
	Modified        []RequirementBlock
	Removed         []string
	Renamed         []Rename
	SkippedHeaders  []SkippedHeader
	SectionPresence SectionPresence
}

// Exactly two #'s — `###` does not match.
var topLevelHeaderTitleRe = regexp.MustCompile(`^##\s+(.+)$`)

var (
	skippedH3Re     = regexp.MustCompile(`^###\s+(.+?)\s*$`)
	removedBulletRe = regexp.MustCompile("^\\s*-\\s*`?###\\s*Requirement:\\s*(.+?)`?\\s*$")
	renamedFromRe   = regexp.MustCompile("^\\s*-?\\s*FROM:\\s*`?###\\s*Requirement:\\s*(.+?)`?\\s*$")
	renamedToRe     = regexp.MustCompile("^\\s*-?\\s*TO:\\s*`?###\\s*Requirement:\\s*(.+?)`?\\s*$")
)

type topLevelSection struct {
	title         string
	body          string
	bodyStartLine int
}

// ParseDeltaSpec parses a delta-formatted spec change file into a DeltaPlan.
func ParseDeltaSpec(content string) DeltaPlan {
	sections := splitTopLevelSections(NormalizeLineEndings(content))

	added, addedFound := sectionCaseInsensitive(sections, "ADDED Requirements")
	modified, modifiedFound := sectionCaseInsensitive(sections, "MODIFIED Requirements")
	removed, removedFound := sectionCaseInsensitive(sections, "REMOVED Requirements")
	renamed, renamedFound := sectionCaseInsensitive(sections, "RENAMED Requirements")

	var skipped []SkippedHeader
	plan := DeltaPlan{
		Added:    parseRequirementBlocksFromSection(added, &skipped),
		Modified: parseRequirementBlocksFromSection(modified, &skipped),
		Removed:  parseRemovedNames(removed.body),
		Renamed:  parseRenamedPairs(renamed.body),
		SectionPresence: SectionPresence{
			Added:    addedFound,
			Modified: modifiedFound,
			Removed:  removedFound,
			Renamed:  renamedFound,
		},
	}
	sortSkippedByLine(skipped)
	plan.SkippedHeaders = skipped
	return plan
}

func splitTopLevelSections(content string) []topLevelSection {
	lines := strings.Split(content, "\n")
	type headerIndex struct {
		title string
		index int
	}
	var indices []headerIndex
	for i, line := range lines {
		if m := topLevelHeaderTitleRe.FindStringSubmatch(line); m != nil {
			indices = append(indices, headerIndex{title: strings.TrimSpace(m[1]), index: i})
		}
	}
	sections := make([]topLevelSection, 0, len(indices))
	for i, current := range indices {
		end := len(lines)
		if i+1 < len(indices) {
			end = indices[i+1].index
		}
		sections = append(sections, topLevelSection{
			title:         current.title,
			body:          strings.Join(lines[current.index+1:end], "\n"),
			bodyStartLine: current.index + 2,
		})
	}
	return sections
}

// Duplicate titles: the last occurrence wins (parity).
func sectionCaseInsensitive(sections []topLevelSection, desired string) (topLevelSection, bool) {
	for i := len(sections) - 1; i >= 0; i-- {
		if strings.EqualFold(sections[i].title, desired) {
			return sections[i], true
		}
	}
	return topLevelSection{title: desired}, false
}

func parseRequirementBlocksFromSection(section topLevelSection, sink *[]SkippedHeader) []RequirementBlock {
	if section.body == "" {
		return nil
	}
	lines := strings.Split(section.body, "\n")
	fenceMask := BuildCodeFenceMask(lines)
	recordIfSkipped := func(index int) {
		if fenceMask[index] {
			return
		}
		m := skippedH3Re.FindStringSubmatch(lines[index])
		if m != nil && !requirementHeaderRe.MatchString(lines[index]) {
			*sink = append(*sink, SkippedHeader{
				Header:  strings.TrimSpace(m[1]),
				Section: section.title,
				Line:    section.bodyStartLine + index,
			})
		}
	}

	var blocks []RequirementBlock
	i := 0
	for i < len(lines) {
		for i < len(lines) && !requirementHeaderRe.MatchString(lines[i]) {
			recordIfSkipped(i)
			i++
		}
		if i >= len(lines) {
			break
		}
		headerLine := lines[i]
		m := requirementHeaderRe.FindStringSubmatch(headerLine)
		if m == nil {
			i++
			continue
		}
		name := NormalizeRequirementName(m[1])
		buf := []string{headerLine}
		i++
		for i < len(lines) &&
			!requirementHeaderRe.MatchString(lines[i]) &&
			!topLevelSectionHeaderRe.MatchString(lines[i]) {
			recordIfSkipped(i)
			buf = append(buf, lines[i])
			i++
		}
		blocks = append(blocks, RequirementBlock{
			HeaderLine: headerLine,
			Name:       name,
			Raw:        strings.TrimRight(strings.Join(buf, "\n"), " \t\n\r"),
		})
	}
	return blocks
}

func parseRemovedNames(sectionBody string) []string {
	if sectionBody == "" {
		return nil
	}
	var names []string
	for _, line := range strings.Split(sectionBody, "\n") {
		if m := requirementHeaderRe.FindStringSubmatch(line); m != nil {
			names = append(names, NormalizeRequirementName(m[1]))
			continue
		}
		if m := removedBulletRe.FindStringSubmatch(line); m != nil {
			names = append(names, NormalizeRequirementName(m[1]))
		}
	}
	return names
}

func parseRenamedPairs(sectionBody string) []Rename {
	if sectionBody == "" {
		return nil
	}
	var pairs []Rename
	var from string
	for _, line := range strings.Split(sectionBody, "\n") {
		if m := renamedFromRe.FindStringSubmatch(line); m != nil {
			from = NormalizeRequirementName(m[1])
			continue
		}
		if m := renamedToRe.FindStringSubmatch(line); m != nil {
			to := NormalizeRequirementName(m[1])
			if from != "" && to != "" {
				pairs = append(pairs, Rename{From: from, To: to})
				from = ""
			}
		}
	}
	return pairs
}

func sortSkippedByLine(skipped []SkippedHeader) {
	for i := 1; i < len(skipped); i++ {
		for j := i; j > 0 && skipped[j].Line < skipped[j-1].Line; j-- {
			skipped[j], skipped[j-1] = skipped[j-1], skipped[j]
		}
	}
}
