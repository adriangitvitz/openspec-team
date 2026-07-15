package change

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adriangitvitz/openspec-go/internal/core"
	"github.com/adriangitvitz/openspec-go/internal/parser"
	"github.com/adriangitvitz/openspec-go/internal/schema"
)

// DependencyInfo is one dependency of an artifact, with completion state.
type DependencyInfo struct {
	ID          string `json:"id"`
	Done        bool   `json:"done"`
	Path        string `json:"path"`
	Description string `json:"description"`
}

// ArtifactInstructions is the `openspec instructions <artifact>` payload.
type ArtifactInstructions struct {
	ChangeName          string                `json:"changeName"`
	ArtifactID          string                `json:"artifactId"`
	SchemaName          string                `json:"schemaName"`
	ChangeDir           string                `json:"changeDir"`
	OutputPath          string                `json:"outputPath"`
	ResolvedOutputPath  string                `json:"resolvedOutputPath"`
	ExistingOutputPaths []string              `json:"existingOutputPaths"`
	Description         string                `json:"description"`
	Instruction         string                `json:"instruction,omitempty"`
	Context             string                `json:"context,omitempty"`
	Rules               []string              `json:"rules,omitempty"`
	Knowledge           []core.KnowledgeEntry `json:"knowledge,omitempty"`
	TeamTestMatrix      string                `json:"teamTestMatrix,omitempty"`
	Template            string                `json:"template"`
	Dependencies        []DependencyInfo      `json:"dependencies"`
	Unlocks             []string              `json:"unlocks"`
}

// BuildInstructions generates enriched instructions for one artifact.
func BuildInstructions(ctx *Context, artifactID string) (*ArtifactInstructions, error) {
	artifact, ok := ctx.Schema.Artifact(artifactID)
	if !ok {
		var valid []string
		for _, a := range ctx.Schema.Artifacts {
			valid = append(valid, a.ID)
		}
		return nil, fmt.Errorf("artifact %q not found in schema %q. Valid artifacts:\n  %s",
			artifactID, ctx.SchemaName, strings.Join(valid, "\n  "))
	}

	template, err := ctx.Schema.TemplateContent(artifact)
	if err != nil {
		return nil, fmt.Errorf("template for artifact %q: %w", artifactID, err)
	}

	var deps []DependencyInfo
	for _, id := range artifact.Requires {
		dep, _ := ctx.Schema.Artifact(id)
		deps = append(deps, DependencyInfo{
			ID:          id,
			Done:        ctx.Completed[id],
			Path:        dep.Generates,
			Description: dep.Description,
		})
	}
	var unlocks []string
	for _, a := range ctx.Schema.Artifacts {
		for _, req := range a.Requires {
			if req == artifactID {
				unlocks = append(unlocks, a.ID)
			}
		}
	}
	sort.Strings(unlocks)

	cfg := core.ReadProjectConfig(ctx.Root)
	existing := schema.ResolveArtifactOutputs(ctx.ChangeDir, artifact.Generates)
	if existing == nil {
		existing = []string{}
	}
	if unlocks == nil {
		unlocks = []string{}
	}
	if deps == nil {
		deps = []DependencyInfo{}
	}

	return &ArtifactInstructions{
		ChangeName:          ctx.ChangeName,
		ArtifactID:          artifact.ID,
		SchemaName:          ctx.SchemaName,
		ChangeDir:           ctx.ChangeDir,
		OutputPath:          artifact.Generates,
		ResolvedOutputPath:  filepath.Join(ctx.ChangeDir, artifact.Generates),
		ExistingOutputPaths: existing,
		Description:         artifact.Description,
		Instruction:         artifact.Instruction,
		Context:             strings.TrimSpace(cfg.Context),
		Rules:               cfg.Rules[artifactID],
		Knowledge:           cfg.Knowledge,
		TeamTestMatrix:      cfg.Team.TestMatrix,
		Template:            string(template),
		Dependencies:        deps,
		Unlocks:             unlocks,
	}, nil
}

// TaskItem is one checkbox in the tracked tasks file.
type TaskItem struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Done        bool   `json:"done"`
}

// Progress tallies task completion.
type Progress struct {
	Total     int `json:"total"`
	Complete  int `json:"complete"`
	Remaining int `json:"remaining"`
}

// ApplyInstructions is the `openspec instructions apply` payload.
type ApplyInstructions struct {
	ChangeName       string                `json:"changeName"`
	ChangeDir        string                `json:"changeDir"`
	SchemaName       string                `json:"schemaName"`
	ContextFiles     map[string][]string   `json:"contextFiles"`
	Progress         Progress              `json:"progress"`
	Tasks            []TaskItem            `json:"tasks"`
	State            string                `json:"state"`
	MissingArtifacts []string              `json:"missingArtifacts,omitempty"`
	Knowledge        []core.KnowledgeEntry `json:"knowledge,omitempty"`
	Instruction      string                `json:"instruction"`
}

// BuildApplyInstructions computes the apply-phase state for a change.
func BuildApplyInstructions(ctx *Context) (*ApplyInstructions, error) {
	requiredIDs := allArtifactIDs(ctx.Schema)
	var tracksFile string
	var schemaInstruction string
	if ctx.Schema.Apply != nil {
		requiredIDs = ctx.Schema.Apply.Requires
		if ctx.Schema.Apply.Tracks != nil {
			tracksFile = *ctx.Schema.Apply.Tracks
		}
		schemaInstruction = strings.TrimSpace(ctx.Schema.Apply.Instruction)
	}

	var missing []string
	for _, id := range requiredIDs {
		if a, ok := ctx.Schema.Artifact(id); ok && !schema.ArtifactOutputExists(ctx.ChangeDir, a.Generates) {
			missing = append(missing, id)
		}
	}

	contextFiles := map[string][]string{}
	for _, a := range ctx.Schema.Artifacts {
		if outputs := schema.ResolveArtifactOutputs(ctx.ChangeDir, a.Generates); len(outputs) > 0 {
			contextFiles[a.ID] = outputs
		}
	}

	tasks := []TaskItem{}
	tracksFileExists := false
	if tracksFile != "" {
		content, err := os.ReadFile(filepath.Join(ctx.ChangeDir, tracksFile))
		if err == nil {
			tracksFileExists = true
			for i, t := range parser.ParseTasks(string(content)) {
				tasks = append(tasks, TaskItem{ID: fmt.Sprintf("%d", i+1), Description: t.Description, Done: t.Done})
			}
		}
	}

	total := len(tasks)
	complete := 0
	for _, t := range tasks {
		if t.Done {
			complete++
		}
	}
	remaining := total - complete

	var state, instruction string
	tracksName := filepath.Base(tracksFile)
	switch {
	case len(missing) > 0:
		state = "blocked"
		instruction = fmt.Sprintf("Cannot apply this change yet. Missing artifacts: %s.\nCreate the missing artifacts first (openspec instructions <artifact> --change %s).",
			strings.Join(missing, ", "), ctx.ChangeName)
	case tracksFile != "" && !tracksFileExists:
		state = "blocked"
		instruction = fmt.Sprintf("The %s file is missing and must be created.\nGenerate the tracking file first (openspec instructions tasks --change %s).", tracksName, ctx.ChangeName)
	case tracksFile != "" && total == 0:
		state = "blocked"
		instruction = fmt.Sprintf("The %s file exists but contains no tasks.\nAdd tasks to %s or regenerate it.", tracksName, tracksName)
	case tracksFile != "" && remaining == 0 && total > 0:
		state = "all_done"
		instruction = "All tasks are complete! This change is ready to be archived.\nConsider running tests and reviewing the changes before archiving."
	default:
		state = "ready"
		if schemaInstruction != "" {
			instruction = schemaInstruction
		} else if tracksFile == "" {
			instruction = "All required artifacts complete. Proceed with implementation."
		} else {
			instruction = "Read context files, work through pending tasks, mark complete as you go.\nPause if you hit blockers or need clarification."
		}
	}

	return &ApplyInstructions{
		ChangeName:       ctx.ChangeName,
		ChangeDir:        ctx.ChangeDir,
		SchemaName:       ctx.SchemaName,
		ContextFiles:     contextFiles,
		Progress:         Progress{Total: total, Complete: complete, Remaining: remaining},
		Tasks:            tasks,
		State:            state,
		MissingArtifacts: missing,
		Knowledge:        core.ReadProjectConfig(ctx.Root).Knowledge,
		Instruction:      instruction,
	}, nil
}
