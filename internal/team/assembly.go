// Package team assembles persona prompts and runs external persona runners
// for the team-driven workflow.
package team

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/adriangitvitz/openspec-team/internal/agents"
	"github.com/adriangitvitz/openspec-team/internal/change"
	"github.com/adriangitvitz/openspec-team/internal/core"
	"github.com/adriangitvitz/openspec-team/internal/schema"
)

const (
	perFileBudget = 32 * 1024
	totalBudget   = 192 * 1024
)

// binaryDocExts lists documents never inlined raw; they are consumed via sibling extractions (<name>.<ext>.md).
var binaryDocExts = map[string]bool{".pdf": true, ".docx": true, ".xlsx": true, ".pptx": true}

// sourceHashRe matches the provenance source-sha256 header; content hashes (not mtimes) are the only git-safe staleness signal.
var sourceHashRe = regexp.MustCompile(`(?i)<!--\s*source-sha256:\s*([0-9a-fA-F]{64})\s*-->`)

// provenanceHeaderCap bounds the header read; provenance lives in the file's first bytes.
const provenanceHeaderCap = 4096

// EvidenceFile is one cited repo file inlined into the prompt.
type EvidenceFile struct {
	Path         string `json:"path"`
	Content      string `json:"content"`
	Truncated    bool   `json:"truncated"`
	ExtractionOf string `json:"extractionOf,omitempty"`
	Stale        bool   `json:"stale,omitempty"`
}

// Bundle is the evidence bundle: inlined files, unresolved citations, binary
// documents awaiting extraction, and citations withheld from external runners.
type Bundle struct {
	Files           []EvidenceFile `json:"files"`
	Unresolved      []string       `json:"unresolved,omitempty"`
	NeedsExtraction []string       `json:"needsExtraction,omitempty"`
	Withheld        []string       `json:"withheld,omitempty"`
}

// DependencyContent is a completed dependency artifact's file content.
type DependencyContent struct {
	ArtifactID string `json:"artifactId"`
	Path       string `json:"path"`
	Content    string `json:"content"`
}

// Assembly is the full persona × artifact prompt package.
type Assembly struct {
	Persona      string                       `json:"persona"`
	ArtifactID   string                       `json:"artifactId"`
	ChangeName   string                       `json:"changeName"`
	SystemPrompt string                       `json:"systemPrompt"`
	Brief        *change.ArtifactInstructions `json:"brief"`
	Dependencies []DependencyContent          `json:"dependencies"`
	Evidence     Bundle                       `json:"evidence"`
}

// Assemble builds the deterministic prompt package for one persona and one
// artifact of a change; every orchestrator consumes this same assembly.
func Assemble(ctx *change.Context, persona, artifactID string) (*Assembly, error) {
	if !slices.Contains(core.PersonaIDs, persona) {
		return nil, fmt.Errorf("unknown persona %q. Valid personas:\n  %s",
			persona, strings.Join(core.PersonaIDs, "\n  "))
	}
	systemPrompt, err := agents.PersonaPrompt(persona)
	if err != nil {
		return nil, err
	}

	artifact, ok := ctx.Schema.Artifact(artifactID)
	if !ok {
		var valid []string
		for _, a := range ctx.Schema.Artifacts {
			valid = append(valid, a.ID)
		}
		return nil, fmt.Errorf("unknown artifact %q in schema %q. Valid artifacts:\n  %s",
			artifactID, ctx.SchemaName, strings.Join(valid, "\n  "))
	}
	for _, dep := range artifact.Requires {
		if !ctx.Completed[dep] {
			return nil, fmt.Errorf("dependency artifact %q is not complete; create it before assembling %q", dep, artifactID)
		}
	}

	brief, err := change.BuildInstructions(ctx, artifactID)
	if err != nil {
		return nil, err
	}

	var confidential []string
	team := core.ReadProjectConfig(ctx.Root).Team
	if runner, _ := team.PersonaRunner(persona); runner != "claude" {
		confidential = team.Confidential
	}

	var deps []DependencyContent
	for _, depID := range artifact.Requires {
		depArtifact, _ := ctx.Schema.Artifact(depID)
		for _, path := range schema.ResolveArtifactOutputs(ctx.ChangeDir, depArtifact.Generates) {
			content, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("dependency %s: %w", depID, err)
			}
			rel, relErr := filepath.Rel(ctx.ChangeDir, path)
			if relErr != nil {
				rel = path
			}
			deps = append(deps, DependencyContent{
				ArtifactID: depID,
				Path:       filepath.ToSlash(rel),
				Content:    string(content),
			})
		}
	}

	return &Assembly{
		Persona:      persona,
		ArtifactID:   artifactID,
		ChangeName:   ctx.ChangeName,
		SystemPrompt: systemPrompt,
		Brief:        brief,
		Dependencies: deps,
		Evidence:     buildBundle(ctx.Root, ctx.ChangeDir, deps, confidential),
	}, nil
}

// A citation is a backticked token in path grammar with a slash or dotted base name; other backticked text is prose.
var (
	backtickRe = regexp.MustCompile("`([^`\n]+)`")
	pathLikeRe = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
)

func isCitation(token string) bool {
	if !pathLikeRe.MatchString(token) || filepath.IsAbs(token) {
		return false
	}
	return strings.Contains(token, "/") || strings.Contains(path.Base(token), ".")
}

// buildBundle resolves citations change-dir-first, then root; confidential matches are withheld (path listed, content absent, no budget spent).
func buildBundle(root, changeDir string, deps []DependencyContent, confidential []string) Bundle {
	var bundle Bundle
	seen := map[string]bool{}
	remaining := totalBudget

	for _, dep := range deps {
		for _, m := range backtickRe.FindAllStringSubmatch(dep.Content, -1) {
			token := m[1]
			if seen[token] || !isCitation(token) {
				continue
			}
			seen[token] = true
			full, ok := resolveCitation(token, changeDir, root)
			if !ok {
				bundle.Unresolved = append(bundle.Unresolved, token)
				continue
			}

			if isConfidential(confidential, root, full) {
				bundle.Withheld = append(bundle.Withheld, token)
				continue
			}
			if binaryDocExts[strings.ToLower(path.Ext(token))] {
				sibToken := token + ".md"
				if seen[sibToken] {
					continue
				}
				if sibling, ok := resolveCitation(sibToken, changeDir, root); ok {
					seen[sibToken] = true
					if isConfidential(confidential, root, sibling) {
						bundle.Withheld = append(bundle.Withheld, sibToken)
						continue
					}
					f := inlineFile(sibling, sibToken, &remaining)
					f.ExtractionOf = token
					f.Stale = extractionIsStale(sibling, full)
					bundle.Files = append(bundle.Files, f)
				} else {
					bundle.NeedsExtraction = append(bundle.NeedsExtraction, token)
				}
				continue
			}
			bundle.Files = append(bundle.Files, inlineFile(full, token, &remaining))
		}
	}
	return bundle
}

// extractionIsStale reads the provenance header from the sibling file itself (bundle truncation cannot hide it); siblings without the hash skip the check.
func extractionIsStale(siblingPath, sourcePath string) bool {
	f, err := os.Open(siblingPath)
	if err != nil {
		return false
	}
	head := make([]byte, provenanceHeaderCap)
	n, _ := io.ReadFull(f, head)
	f.Close()
	m := sourceHashRe.FindSubmatch(head[:n])
	if m == nil {
		return false
	}
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return false
	}
	current := fmt.Sprintf("%x", sha256.Sum256(source))
	return current != strings.ToLower(string(m[1]))
}

// resolveCitation tries each base with the tool loop's symlink-safe containment and returns the first regular file.
func resolveCitation(token string, bases ...string) (string, bool) {
	for _, base := range bases {
		full, err := resolveInRoot(base, token)
		if err != nil {
			continue
		}
		if info, err := os.Stat(full); err == nil && info.Mode().IsRegular() {
			return full, true
		}
	}
	return "", false
}

func inlineFile(full, token string, remaining *int) EvidenceFile {
	budget := min(perFileBudget, *remaining)
	if budget <= 0 {
		return EvidenceFile{Path: token, Truncated: true}
	}
	content, err := os.ReadFile(full)
	if err != nil {
		return EvidenceFile{Path: token, Truncated: true}
	}
	truncated := len(content) > budget
	if truncated {
		content = content[:budget]
	}
	*remaining -= len(content)
	return EvidenceFile{Path: token, Content: string(content), Truncated: truncated}
}

// Render formats the assembly as the persona's prompt text.
func Render(a *Assembly) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Persona: %s — artifact: %s (change: %s)\n\n", a.Persona, a.ArtifactID, a.ChangeName)
	b.WriteString("## System prompt\n\n")
	b.WriteString(strings.TrimSpace(a.SystemPrompt))
	b.WriteString("\n\n## Artifact brief\n\n")
	b.WriteString(strings.TrimSpace(a.Brief.Instruction))
	fmt.Fprintf(&b, "\n\n### Output\n\nWrite the artifact for output path pattern: %s\n", a.Brief.OutputPath)
	fmt.Fprintf(&b, "\n### Template\n\n%s\n", strings.TrimSpace(a.Brief.Template))
	if a.Brief.Context != "" {
		fmt.Fprintf(&b, "\n### Project context\n\n%s\n", a.Brief.Context)
	}
	for _, rule := range a.Brief.Rules {
		fmt.Fprintf(&b, "\n### Rule\n\n%s\n", rule)
	}
	if a.Brief.TeamTestMatrix != "" {
		fmt.Fprintf(&b, "\n### Referenced test matrix\n\n%s\n", a.Brief.TeamTestMatrix)
	}
	for _, dep := range a.Dependencies {
		fmt.Fprintf(&b, "\n## Dependency artifact: %s (%s)\n\n%s\n", dep.ArtifactID, dep.Path, strings.TrimSpace(dep.Content))
	}
	if len(a.Evidence.Files) > 0 || len(a.Evidence.Unresolved) > 0 || len(a.Evidence.NeedsExtraction) > 0 || len(a.Evidence.Withheld) > 0 {
		b.WriteString("\n## Evidence bundle\n")
		b.WriteString("\nCited files inlined below. Verify claims against them.\n")
		for _, f := range a.Evidence.Files {
			fmt.Fprintf(&b, "\n### %s\n\n", f.Path)
			if f.ExtractionOf != "" {
				fmt.Fprintf(&b, "[extraction of %s — cite by its section/page anchors]\n", f.ExtractionOf)
			}
			if f.Stale {
				fmt.Fprintf(&b, "[stale extraction: %s was modified after this extraction was taken]\n", f.ExtractionOf)
			}
			fmt.Fprintf(&b, "```\n%s\n```\n", f.Content)
			if f.Truncated {
				fmt.Fprintf(&b, "\n[truncated: %s exceeds the evidence budget]\n", f.Path)
			}
		}
		if len(a.Evidence.Withheld) > 0 {
			b.WriteString("\n### Withheld citations\n\nThese cited files are confidential and withheld from this runner. Do not guess or infer their contents; if one is genuinely needed for a decision, record it as an open question and ask the human at the gate for a curated release:\n\n")
			for _, w := range a.Evidence.Withheld {
				fmt.Fprintf(&b, "- %s\n", w)
			}
		}
		if len(a.Evidence.NeedsExtraction) > 0 {
			b.WriteString("\n### Citations needing extraction\n\nThese binary documents exist but have no sibling extraction (<name>.<ext>.md). Ask for extraction instead of guessing their contents:\n\n")
			for _, n := range a.Evidence.NeedsExtraction {
				fmt.Fprintf(&b, "- %s\n", n)
			}
		}
		if len(a.Evidence.Unresolved) > 0 {
			b.WriteString("\n### Unresolved citations\n\nThese cited paths do not exist in the repo — flag them instead of assuming their contents:\n\n")
			for _, u := range a.Evidence.Unresolved {
				fmt.Fprintf(&b, "- %s\n", u)
			}
		}
	}
	return b.String()
}
