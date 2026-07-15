package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/adriangitvitz/openspec-go/internal/core"
)

var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "from": true,
	"that": true, "this": true, "into": true, "are": true, "its": true,
	"not": true, "was": true, "were": true, "has": true, "have": true,
}

var tokenSplitRe = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func topicTerms(topic string) []string {
	var terms []string
	for _, tok := range tokenSplitRe.Split(topic, -1) {
		tok = strings.ToLower(tok)
		if len(tok) >= 3 && !stopwords[tok] {
			terms = append(terms, tok)
		}
	}
	return terms
}

// KnowledgeCoverage warns when a knowledge topic's terms appear in the change's
// artifacts but none of the topic's paths are cited.
func KnowledgeCoverage(changeDir string, entries []core.KnowledgeEntry) []Issue {
	if len(entries) == 0 {
		return nil
	}

	artifactText, researchText, hasResearch := readChangeTexts(changeDir)
	if artifactText == "" {
		return nil
	}
	citationText := researchText
	citationScope := "research.md"
	if !hasResearch {

		citationText = artifactText + "\n" + researchText
		citationScope = "the change's artifacts"
	}
	lowerCitations := strings.ToLower(citationText)

	var issues []Issue
	for _, entry := range entries {
		if len(entry.Paths) == 0 {
			continue
		}
		term, implicated := firstMatchingTerm(entry.Topic, artifactText)
		if !implicated {
			continue
		}
		if anyPathCited(entry.Paths, lowerCitations) {
			continue
		}
		issues = append(issues, Issue{
			Level: Warning,
			Path:  "knowledge-coverage",
			Message: fmt.Sprintf(
				"knowledge topic %q appears related to this change (term %q found in its artifacts) but none of its paths are cited in %s. Read the mapped docs and cite them, or record why they do not apply.",
				entry.Topic, term, citationScope),
		})
	}
	return issues
}

func readChangeTexts(changeDir string) (artifactText, researchText string, hasResearch bool) {
	var artifacts, research []string
	filepath.WalkDir(changeDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if d.Name() == "research.md" {
			research = append(research, string(content))
		} else {
			artifacts = append(artifacts, string(content))
		}
		return nil
	})
	return strings.Join(artifacts, "\n"), strings.Join(research, "\n"), len(research) > 0
}

func firstMatchingTerm(topic, text string) (string, bool) {
	for _, term := range topicTerms(topic) {
		re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(term) + `\b`)
		if re.MatchString(text) {
			return term, true
		}
	}
	return "", false
}

// anyPathCited accepts the full path, or the basename for files only (dir basenames like "adr" are too generic).
func anyPathCited(paths []string, lowerText string) bool {
	for _, p := range paths {
		if strings.Contains(lowerText, strings.ToLower(filepath.ToSlash(p))) {
			return true
		}
		base := filepath.Base(p)
		if strings.Contains(base, ".") && strings.Contains(lowerText, strings.ToLower(base)) {
			return true
		}
	}
	return false
}

// NewReport builds a report from issues with strict-mode handling.
func NewReport(issues []Issue, strict bool) Report {
	return report(issues, strict)
}
