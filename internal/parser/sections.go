package parser

import (
	"regexp"
	"strings"
)

var headerRe = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)

// Section is a header plus the content until the next header of equal or
// higher level; fenced headers do not open sections.
type Section struct {
	Level    int
	Title    string
	Content  string
	Children []*Section
}

// ParseSections builds the section tree for a document.
func ParseSections(content string) []*Section {
	lines := strings.Split(NormalizeLineEndings(content), "\n")
	mask := BuildCodeFenceMask(lines)

	var sections []*Section
	var stack []*Section

	for i, line := range lines {
		if mask[i] {
			continue
		}
		m := headerRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		level := len(m[1])
		section := &Section{
			Level:   level,
			Title:   strings.TrimSpace(m[2]),
			Content: contentUntilNextHeader(lines, mask, i+1, level),
		}

		for len(stack) > 0 && stack[len(stack)-1].Level >= level {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			sections = append(sections, section)
		} else {
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, section)
		}
		stack = append(stack, section)
	}
	return sections
}

func contentUntilNextHeader(lines []string, mask []bool, start, currentLevel int) string {
	var content []string
	for i := start; i < len(lines); i++ {
		if !mask[i] {
			if m := headerRe.FindStringSubmatch(lines[i]); m != nil && len(m[1]) <= currentLevel {
				break
			}
		}
		content = append(content, lines[i])
	}
	return strings.TrimSpace(strings.Join(content, "\n"))
}

// FindSection looks up a section by title, case-insensitively, depth-first.
func FindSection(sections []*Section, title string) *Section {
	for _, s := range sections {
		if strings.EqualFold(s.Title, title) {
			return s
		}
		if child := FindSection(s.Children, title); child != nil {
			return child
		}
	}
	return nil
}
