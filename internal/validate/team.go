package validate

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"

	"github.com/adriangitvitz/openspec-go/internal/core"
)

// TeamConfigReport wraps TeamConfigIssues into a Report; false when there is no
// team section. Any populated team field must reach the report path, or its warnings are unreachable.
func TeamConfigReport(root string, team core.TeamConfig, strict bool) (Report, bool) {
	if len(team.Personas) == 0 && team.TestMatrix == "" && team.Search.MCPURL == "" && len(team.Confidential) == 0 {
		return Report{}, false
	}
	return NewReport(TeamConfigIssues(root, team), strict), true
}

// TeamConfigIssues checks the team section of config.yaml; all issues are warnings.
func TeamConfigIssues(root string, team core.TeamConfig) []Issue {
	known := map[string]bool{}
	for _, id := range core.PersonaIDs {
		known[id] = true
	}

	var issues []Issue
	for _, id := range sortedPersonaKeys(team.Personas) {
		p := team.Personas[id]
		if !known[id] {
			issues = append(issues, Issue{
				Level:   Warning,
				Path:    "team",
				Message: fmt.Sprintf("team.personas: unknown persona %q (known: %v)", id, core.PersonaIDs),
			})
		}
		switch p.Runner {
		case "", "claude":
		case "openrouter":
			if p.Model == "" {
				issues = append(issues, Issue{
					Level:   Warning,
					Path:    "team",
					Message: fmt.Sprintf("team.personas.%s: runner openrouter requires a model", id),
				})
			}
		default:
			issues = append(issues, Issue{
				Level:   Warning,
				Path:    "team",
				Message: fmt.Sprintf("team.personas.%s: unknown runner %q (claude or openrouter)", id, p.Runner),
			})
		}
	}

	if team.TestMatrix != "" {
		full := team.TestMatrix
		if !filepath.IsAbs(full) {
			full = filepath.Join(root, team.TestMatrix)
		}
		if _, err := os.Stat(full); err != nil {
			issues = append(issues, Issue{
				Level:   Warning,
				Path:    "team",
				Message: fmt.Sprintf("team.test_matrix: path does not exist: %s", team.TestMatrix),
			})
		}
	}

	if team.Search.MCPURL != "" {
		if u, err := url.Parse(team.Search.MCPURL); err != nil || u.Scheme == "" || u.Host == "" {
			issues = append(issues, Issue{
				Level:   Warning,
				Path:    "team",
				Message: fmt.Sprintf("team.search.mcp_url is not a valid URL (need scheme and host): %s", team.Search.MCPURL),
			})
		}
	}

	if len(team.Confidential) > 0 {
		files := projectFiles(root)
		for _, pattern := range team.Confidential {
			if err := core.CheckConfidentialPattern(pattern); err != nil {
				issues = append(issues, Issue{
					Level:   Warning,
					Path:    "team",
					Message: fmt.Sprintf("team.confidential: malformed %v (it will match everything — fail closed)", err),
				})
				continue
			}
			matched := false
			for _, f := range files {
				if core.MatchesConfidential([]string{pattern}, f) {
					matched = true
					break
				}
			}
			if !matched {
				issues = append(issues, Issue{
					Level:   Warning,
					Path:    "team",
					Message: fmt.Sprintf("team.confidential: pattern %q matches no existing file (typo?)", pattern),
				})
			}
		}
	}
	return issues
}

// projectFiles lists slash-normalized root-relative files, skipping ExcludedScanDirs.
func projectFiles(root string) []string {
	skip := map[string]bool{}
	for _, d := range core.ExcludedScanDirs {
		skip[d] = true
	}
	var files []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skip[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if rel, err := filepath.Rel(root, p); err == nil {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	return files
}

func sortedPersonaKeys(personas map[string]core.PersonaConfig) []string {
	keys := make([]string, 0, len(personas))
	for k := range personas {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
