// Package agents generates AI-tool integration files (skills and slash
// commands) from embedded assets; supported tools are entries in Tools.
package agents

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/adriangitvitz/openspec-team/internal/core"
	"github.com/adriangitvitz/openspec-team/internal/fsutil"
)

//go:embed assets
var assets embed.FS

// Workflow maps a workflow id to its skill directory name.
type Workflow struct {
	ID       string
	SkillDir string
}

// Workflows is the shipped set.
var Workflows = []Workflow{
	{ID: "propose", SkillDir: "openspec-propose"},
	{ID: "explore", SkillDir: "openspec-explore"},
	{ID: "apply", SkillDir: "openspec-apply-change"},
	{ID: "update", SkillDir: "openspec-update-change"},
	{ID: "sync", SkillDir: "openspec-sync-specs"},
	{ID: "verify", SkillDir: "openspec-verify-change"},
	{ID: "archive", SkillDir: "openspec-archive-change"},
	{ID: "team", SkillDir: "openspec-team"},
}

// Tool describes where one AI tool expects its skill and command files.
type Tool struct {
	Name        string
	SkillsDir   string
	CommandsDir string
	AgentsDir   string
	RawCommands bool
}

// Tools is the registry of supported AI tools. Crush reads .claude/skills
// natively, so it only needs its own command files (project:opsx:<name>).
var Tools = []Tool{
	{Name: "claude", SkillsDir: ".claude/skills", CommandsDir: ".claude/commands/opsx", AgentsDir: ".claude/agents"},
	{Name: "crush", CommandsDir: ".crush/commands/opsx", RawCommands: true},
}

// PersonaPrompt returns a persona's system prompt: the agent asset body with frontmatter stripped.
func PersonaPrompt(persona string) (string, error) {
	raw, err := assets.ReadFile("assets/agents/opsx-" + persona + ".md")
	if err != nil {
		return "", fmt.Errorf("unknown persona %q: %w", persona, err)
	}
	return string(stripFrontmatter(raw)), nil
}

// Generate writes every tool's skill, command, and agent files, overwriting stale versions (machine-owned).
func Generate(projectRoot, version string) ([]string, error) {
	data := struct{ Version string }{Version: version}
	var written []string

	for _, tool := range Tools {
		if tool.AgentsDir != "" {
			for _, persona := range core.PersonaIDs {
				content, err := renderAsset("assets/agents/opsx-"+persona+".md", data)
				if err != nil {
					return written, err
				}
				path := filepath.Join(projectRoot, tool.AgentsDir, "opsx-"+persona+".md")
				if err := writeGenerated(path, content); err != nil {
					return written, err
				}
				written = append(written, path)
			}
		}
		for _, wf := range Workflows {
			if tool.SkillsDir != "" {
				content, err := renderAsset("assets/skills/"+wf.ID+".md", data)
				if err != nil {
					return written, err
				}
				path := filepath.Join(projectRoot, tool.SkillsDir, wf.SkillDir, "SKILL.md")
				if err := writeGenerated(path, content); err != nil {
					return written, err
				}
				written = append(written, path)
			}
			if tool.CommandsDir != "" {
				content, err := renderAsset("assets/commands/"+wf.ID+".md", data)
				if err != nil {
					return written, err
				}
				if tool.RawCommands {
					content = stripFrontmatter(content)
				}
				path := filepath.Join(projectRoot, tool.CommandsDir, wf.ID+".md")
				if err := writeGenerated(path, content); err != nil {
					return written, err
				}
				written = append(written, path)
			}
		}
	}
	return written, nil
}

func renderAsset(assetPath string, data any) ([]byte, error) {
	raw, err := assets.ReadFile(assetPath)
	if err != nil {
		return nil, fmt.Errorf("embedded asset %s: %w", assetPath, err)
	}
	tmpl, err := template.New(filepath.Base(assetPath)).Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("asset template %s: %w", assetPath, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func stripFrontmatter(content []byte) []byte {
	if !bytes.HasPrefix(content, []byte("---\n")) {
		return content
	}
	rest := content[4:]
	end := bytes.Index(rest, []byte("\n---\n"))
	if end < 0 {
		return content
	}
	return bytes.TrimLeft(rest[end+5:], "\n")
}

func writeGenerated(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, content, 0o644)
}
