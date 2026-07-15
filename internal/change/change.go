// Package change orchestrates the change workflow: create, status,
// instructions, list, and archive.
package change

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/adriangitvitz/openspec-team/internal/core"
	"github.com/adriangitvitz/openspec-team/internal/schema"
)

// Context bundles everything commands need about one change.
type Context struct {
	Root       string
	ChangeName string
	ChangeDir  string
	SchemaName string
	Schema     *schema.Schema
	Completed  map[string]bool
}

// LoadContext resolves a change's schema and detects artifact completion.
func LoadContext(root, changeName, schemaOverride string) (*Context, error) {
	changeDir := filepath.Join(core.ChangesDir(root), changeName)
	if info, err := os.Stat(changeDir); err != nil || !info.IsDir() {
		available := ActiveChangeNames(root)
		if len(available) > 0 {
			return nil, fmt.Errorf("change %q not found. Active changes:\n  %s", changeName, joinLines(available))
		}
		return nil, fmt.Errorf("change %q not found. Create one with: openspec new change %s", changeName, changeName)
	}

	schemaName := core.ResolveSchemaForChange(changeDir, schemaOverride, root)
	s, err := schema.Resolve(schemaName, root)
	if err != nil {
		return nil, err
	}

	completed := map[string]bool{}
	for _, a := range s.Artifacts {
		if schema.ArtifactOutputExists(changeDir, a.Generates) {
			completed[a.ID] = true
		}
	}

	return &Context{
		Root:       root,
		ChangeName: changeName,
		ChangeDir:  changeDir,
		SchemaName: schemaName,
		Schema:     s,
		Completed:  completed,
	}, nil
}

// ActiveChangeNames lists non-archive change directories, sorted.
func ActiveChangeNames(root string) []string {
	entries, err := os.ReadDir(core.ChangesDir(root))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && e.Name() != "archive" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func joinLines(items []string) string {
	out := ""
	for i, item := range items {
		if i > 0 {
			out += "\n  "
		}
		out += item
	}
	return out
}

// CreateOptions configure Create.
type CreateOptions struct {
	Schema      string
	Description string
	Goal        string
}

// CreateResult reports what Create produced.
type CreateResult struct {
	ID           string `json:"id"`
	Path         string `json:"path"`
	MetadataPath string `json:"metadataPath"`
	Schema       string `json:"schema"`
}

// Create makes openspec/changes/<name>/ with its .openspec.yaml metadata.
func Create(root, name string, opts CreateOptions) (*CreateResult, error) {
	if err := core.ValidateChangeName(name); err != nil {
		return nil, err
	}

	schemaName := opts.Schema
	if schemaName == "" {
		schemaName = core.ReadProjectConfig(root).Schema
	}
	if _, err := schema.Resolve(schemaName, root); err != nil {
		return nil, err
	}

	changeDir := filepath.Join(core.ChangesDir(root), name)
	if _, err := os.Stat(changeDir); err == nil {
		return nil, fmt.Errorf("change %q already exists at %s", name, changeDir)
	}
	if err := os.MkdirAll(changeDir, 0o755); err != nil {
		return nil, err
	}
	meta := core.ChangeMetadata{Schema: schemaName, Goal: opts.Goal}
	if err := core.WriteChangeMetadata(changeDir, meta); err != nil {
		return nil, err
	}
	if opts.Description != "" {
		readme := fmt.Sprintf("# %s\n\n%s\n", name, opts.Description)
		if err := os.WriteFile(filepath.Join(changeDir, "README.md"), []byte(readme), 0o644); err != nil {
			return nil, err
		}
	}
	return &CreateResult{
		ID:           name,
		Path:         changeDir,
		MetadataPath: filepath.Join(changeDir, core.MetadataFilename),
		Schema:       schemaName,
	}, nil
}
