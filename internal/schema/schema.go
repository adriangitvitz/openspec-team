// Package schema loads artifact schemas: definitions, templates, and the dependency
// graph. Built-ins are embedded; projects can override under openspec/schemas/<name>/.
package schema

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
)

//go:embed assets
var embedded embed.FS

// Artifact is one artifact definition in a schema.
type Artifact struct {
	ID          string   `yaml:"id" json:"id"`
	Generates   string   `yaml:"generates" json:"generates"`
	Description string   `yaml:"description" json:"description"`
	Template    string   `yaml:"template" json:"template"`
	Instruction string   `yaml:"instruction" json:"instruction,omitempty"`
	Requires    []string `yaml:"requires" json:"requires"`
}

// ApplyPhase configures schema-aware apply instructions.
type ApplyPhase struct {
	Requires     []string `yaml:"requires" json:"requires"`
	Tracks       *string  `yaml:"tracks" json:"tracks,omitempty"`
	Instruction  string   `yaml:"instruction" json:"instruction,omitempty"`
	Traceability bool     `yaml:"traceability" json:"traceability,omitempty"`
}

// Schema is a parsed, validated schema.yaml.
type Schema struct {
	Name        string      `yaml:"name" json:"name"`
	Version     int         `yaml:"version" json:"version"`
	Description string      `yaml:"description" json:"description,omitempty"`
	Artifacts   []Artifact  `yaml:"artifacts" json:"artifacts"`
	Apply       *ApplyPhase `yaml:"apply" json:"apply,omitempty"`

	dir string
}

// Artifact returns the artifact with the given id, if present.
func (s *Schema) Artifact(id string) (Artifact, bool) {
	for _, a := range s.Artifacts {
		if a.ID == id {
			return a, true
		}
	}
	return Artifact{}, false
}

// TrackedTasksArtifact matches generates against apply.tracks, falling back
// to the artifact with id "tasks".
func (s *Schema) TrackedTasksArtifact() (Artifact, bool) {
	if s.Apply != nil && s.Apply.Tracks != nil {
		for _, a := range s.Artifacts {
			if a.Generates == *s.Apply.Tracks {
				return a, true
			}
		}
		return Artifact{}, false
	}
	return s.Artifact("tasks")
}

// Parse parses and validates schema.yaml content.
func Parse(content []byte) (*Schema, error) {
	var s Schema
	if err := yaml.Unmarshal(content, &s); err != nil {
		return nil, fmt.Errorf("invalid schema: %w", err)
	}
	if s.Name == "" {
		return nil, fmt.Errorf("invalid schema: name is required")
	}
	if s.Version <= 0 {
		return nil, fmt.Errorf("invalid schema: version must be a positive integer")
	}
	if len(s.Artifacts) == 0 {
		return nil, fmt.Errorf("invalid schema: at least one artifact required")
	}
	ids := make(map[string]bool, len(s.Artifacts))
	for _, a := range s.Artifacts {
		switch {
		case a.ID == "":
			return nil, fmt.Errorf("invalid schema: artifact ID is required")
		case a.Generates == "":
			return nil, fmt.Errorf("invalid schema: artifact %q: generates field is required", a.ID)
		case a.Template == "":
			return nil, fmt.Errorf("invalid schema: artifact %q: template field is required", a.ID)
		case ids[a.ID]:
			return nil, fmt.Errorf("invalid schema: duplicate artifact ID: %s", a.ID)
		}
		ids[a.ID] = true
	}
	for _, a := range s.Artifacts {
		for _, req := range a.Requires {
			if !ids[req] {
				return nil, fmt.Errorf("invalid schema: artifact %q requires %q, which does not exist", a.ID, req)
			}
		}
	}
	if s.Apply != nil {
		if len(s.Apply.Requires) == 0 {
			return nil, fmt.Errorf("invalid schema: apply.requires needs at least one artifact")
		}
		for _, req := range s.Apply.Requires {
			if !ids[req] {
				return nil, fmt.Errorf("invalid schema: apply requires %q, which does not exist", req)
			}
		}
	}
	if cycle := findCycle(s.Artifacts); cycle != "" {
		return nil, fmt.Errorf("invalid schema: cyclic dependency detected: %s", cycle)
	}
	return &s, nil
}

// Resolve loads a schema by name: project override first, then embedded.
func Resolve(name, projectRoot string) (*Schema, error) {
	name = strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml")

	if projectRoot != "" {
		dir := filepath.Join(projectRoot, "openspec", "schemas", name)
		if content, err := os.ReadFile(filepath.Join(dir, "schema.yaml")); err == nil {
			s, err := Parse(content)
			if err != nil {
				return nil, fmt.Errorf("schema %q at %s: %w", name, dir, err)
			}
			s.dir = dir
			return s, nil
		}
	}

	content, err := embedded.ReadFile("assets/" + name + "/schema.yaml")
	if err != nil {
		return nil, fmt.Errorf("schema %q not found (available: %s)", name, strings.Join(List(projectRoot), ", "))
	}
	return Parse(content)
}

// TemplateContent returns the raw template file content for an artifact.
func (s *Schema) TemplateContent(a Artifact) ([]byte, error) {
	if s.dir != "" {
		return os.ReadFile(filepath.Join(s.dir, "templates", a.Template))
	}
	return embedded.ReadFile("assets/" + s.Name + "/templates/" + a.Template)
}

// List returns available schema names, sorted and deduplicated.
func List(projectRoot string) []string {
	names := map[string]bool{}
	if entries, err := embedded.ReadDir("assets"); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				names[e.Name()] = true
			}
		}
	}
	if projectRoot != "" {
		dir := filepath.Join(projectRoot, "openspec", "schemas")
		if entries, err := os.ReadDir(dir); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					if _, err := os.Stat(filepath.Join(dir, e.Name(), "schema.yaml")); err == nil {
						names[e.Name()] = true
					}
				}
			}
		}
	}
	out := make([]string, 0, len(names))
	for n := range names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func findCycle(artifacts []Artifact) string {
	byID := make(map[string]Artifact, len(artifacts))
	for _, a := range artifacts {
		byID[a.ID] = a
	}
	const (
		unvisited = 0
		inStack   = 1
		done      = 2
	)
	state := map[string]int{}
	var path []string

	var dfs func(id string) string
	dfs = func(id string) string {
		state[id] = inStack
		path = append(path, id)
		for _, dep := range byID[id].Requires {
			switch state[dep] {
			case inStack:
				start := 0
				for i, p := range path {
					if p == dep {
						start = i
						break
					}
				}
				return strings.Join(append(path[start:], dep), " → ")
			case unvisited:
				if cycle := dfs(dep); cycle != "" {
					return cycle
				}
			}
		}
		state[id] = done
		path = path[:len(path)-1]
		return ""
	}

	for _, a := range artifacts {
		if state[a.ID] == unvisited {
			if cycle := dfs(a.ID); cycle != "" {
				return cycle
			}
		}
	}
	return ""
}
