// Package core resolves the OpenSpec root and reads project and change
// configuration files.
package core

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
)

// DefaultSchema is used when neither the change metadata nor the project config names a schema.
const DefaultSchema = "spec-driven"

// FindRoot walks up from start to the nearest directory containing openspec/.
func FindRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		info, err := os.Stat(filepath.Join(dir, "openspec"))
		if err == nil && info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no openspec/ directory found in %s or any parent. Run 'openspec init' first", start)
		}
		dir = parent
	}
}

func OpenSpecDir(root string) string { return filepath.Join(root, "openspec") }

func ChangesDir(root string) string { return filepath.Join(root, "openspec", "changes") }

func SpecsDir(root string) string { return filepath.Join(root, "openspec", "specs") }

func ArchiveDir(root string) string { return filepath.Join(root, "openspec", "changes", "archive") }

// KnowledgeEntry maps a domain topic to the files that hold its truth.
type KnowledgeEntry struct {
	Topic string   `yaml:"topic" json:"topic"`
	Note  string   `yaml:"note,omitempty" json:"note,omitempty"`
	Paths []string `yaml:"paths" json:"paths"`
}

// PersonaConfig selects a persona's runner: "claude" (default) or "openrouter" (requires Model).
type PersonaConfig struct {
	Runner string `yaml:"runner,omitempty" json:"runner,omitempty"`
	Model  string `yaml:"model,omitempty" json:"model,omitempty"`
}

// SearchConfig points at a self-hosted search MCP server; the bearer token comes
// only from OPENSPEC_SEARCH_TOKEN — the schema deliberately has no token field.
type SearchConfig struct {
	MCPURL string `yaml:"mcp_url,omitempty" json:"mcpUrl,omitempty"`
}

// TeamConfig is the team section of config.yaml (team-driven schema).
type TeamConfig struct {
	TestMatrix   string                   `yaml:"test_matrix,omitempty" json:"testMatrix,omitempty"`
	Personas     map[string]PersonaConfig `yaml:"personas,omitempty" json:"personas,omitempty"`
	Search       SearchConfig             `yaml:"search,omitempty" json:"search,omitempty"`
	Confidential []string                 `yaml:"confidential,omitempty" json:"confidential,omitempty"`
}

// PersonaIDs is the canonical team-driven persona set.
var PersonaIDs = []string{
	"product-owner", "senior-staff", "senior-engineer",
	"backend-dev", "frontend-dev", "qa", "ui-ux",
}

// PersonaRunner resolves a persona's runner and model; undeclared personas default to "claude".
func (t TeamConfig) PersonaRunner(id string) (runner, model string) {
	p, ok := t.Personas[id]
	if !ok || p.Runner == "" {
		return "claude", p.Model
	}
	return p.Runner, p.Model
}

// ProjectConfig is openspec/config.yaml.
type ProjectConfig struct {
	Schema    string              `yaml:"schema"`
	Context   string              `yaml:"context,omitempty"`
	Rules     map[string][]string `yaml:"rules,omitempty"`
	Knowledge []KnowledgeEntry    `yaml:"knowledge,omitempty"`
	Team      TeamConfig          `yaml:"team,omitempty"`
}

// ReadProjectConfig loads openspec/config.yaml, falling back to defaults if missing or unparseable.
func ReadProjectConfig(root string) ProjectConfig {
	cfg := ProjectConfig{Schema: DefaultSchema}
	content, err := os.ReadFile(filepath.Join(OpenSpecDir(root), "config.yaml"))
	if err != nil {
		return cfg
	}
	var parsed ProjectConfig
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		return cfg
	}
	if parsed.Schema == "" {
		parsed.Schema = DefaultSchema
	}
	return parsed
}

// ChangeMetadata is a change's .openspec.yaml.
type ChangeMetadata struct {
	Schema string `yaml:"schema"`
	Goal   string `yaml:"goal,omitempty"`
}

// MetadataFilename is the change metadata file name.
const MetadataFilename = ".openspec.yaml"

// ReadChangeMetadata loads a change's .openspec.yaml, or nil if absent.
func ReadChangeMetadata(changeDir string) (*ChangeMetadata, error) {
	content, err := os.ReadFile(filepath.Join(changeDir, MetadataFilename))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var meta ChangeMetadata
	if err := yaml.Unmarshal(content, &meta); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", MetadataFilename, err)
	}
	return &meta, nil
}

// WriteChangeMetadata writes a change's .openspec.yaml.
func WriteChangeMetadata(changeDir string, meta ChangeMetadata) error {
	content, err := yaml.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(changeDir, MetadataFilename), content, 0o644)
}

// ResolveSchemaForChange resolves the schema: override, then change metadata, then project config.
func ResolveSchemaForChange(changeDir, override, root string) string {
	if override != "" {
		return override
	}
	if meta, err := ReadChangeMetadata(changeDir); err == nil && meta != nil && meta.Schema != "" {
		return meta.Schema
	}
	return ReadProjectConfig(root).Schema
}

var kebabCaseRe = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// ValidateChangeName enforces kebab-case with targeted error messages.
func ValidateChangeName(name string) error {
	if name == "" {
		return fmt.Errorf("change name cannot be empty")
	}
	if kebabCaseRe.MatchString(name) {
		return nil
	}
	switch {
	case regexp.MustCompile(`[A-Z]`).MatchString(name):
		return fmt.Errorf("change name must be lowercase (use kebab-case)")
	case regexp.MustCompile(`\s`).MatchString(name):
		return fmt.Errorf("change name cannot contain spaces (use hyphens instead)")
	case strings.Contains(name, "_"):
		return fmt.Errorf("change name cannot contain underscores (use hyphens instead)")
	case strings.HasPrefix(name, "-"):
		return fmt.Errorf("change name cannot start with a hyphen")
	case strings.HasSuffix(name, "-"):
		return fmt.Errorf("change name cannot end with a hyphen")
	case strings.Contains(name, "--"):
		return fmt.Errorf("change name cannot contain consecutive hyphens")
	case regexp.MustCompile(`^[0-9]`).MatchString(name):
		return fmt.Errorf("change name must start with a letter")
	case regexp.MustCompile(`[^a-z0-9-]`).MatchString(name):
		return fmt.Errorf("change name can only contain lowercase letters, numbers, and hyphens")
	default:
		return fmt.Errorf("change name must follow kebab-case convention (e.g., add-auth, refactor-db)")
	}
}
