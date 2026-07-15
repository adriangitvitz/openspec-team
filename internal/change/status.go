package change

import (
	"path/filepath"

	"github.com/adriangitvitz/openspec-team/internal/schema"
)

// ArtifactStatus is one artifact's workflow state.
type ArtifactStatus struct {
	ID          string   `json:"id"`
	OutputPath  string   `json:"outputPath"`
	Status      string   `json:"status"`
	MissingDeps []string `json:"missingDeps,omitempty"`
}

// ArtifactPathSummary resolves an artifact's outputs under the change dir.
type ArtifactPathSummary struct {
	OutputPath          string   `json:"outputPath"`
	ResolvedOutputPath  string   `json:"resolvedOutputPath"`
	ExistingOutputPaths []string `json:"existingOutputPaths"`
}

// Status is the `openspec status` payload the skills parse.
type Status struct {
	ChangeName    string                         `json:"changeName"`
	SchemaName    string                         `json:"schemaName"`
	ChangeRoot    string                         `json:"changeRoot"`
	ArtifactPaths map[string]ArtifactPathSummary `json:"artifactPaths"`
	IsComplete    bool                           `json:"isComplete"`
	ApplyRequires []string                       `json:"applyRequires"`
	Artifacts     []ArtifactStatus               `json:"artifacts"`
}

// BuildStatus computes artifact statuses in build order.
func BuildStatus(ctx *Context) *Status {
	applyRequires := allArtifactIDs(ctx.Schema)
	if ctx.Schema.Apply != nil {
		applyRequires = ctx.Schema.Apply.Requires
	}

	ready := map[string]bool{}
	for _, id := range ctx.Schema.NextArtifacts(ctx.Completed) {
		ready[id] = true
	}
	blocked := ctx.Schema.Blocked(ctx.Completed)

	paths := map[string]ArtifactPathSummary{}
	byID := map[string]ArtifactStatus{}
	for _, a := range ctx.Schema.Artifacts {
		existing := schema.ResolveArtifactOutputs(ctx.ChangeDir, a.Generates)
		if existing == nil {
			existing = []string{}
		}
		paths[a.ID] = ArtifactPathSummary{
			OutputPath:          a.Generates,
			ResolvedOutputPath:  filepath.Join(ctx.ChangeDir, a.Generates),
			ExistingOutputPaths: existing,
		}
		st := ArtifactStatus{ID: a.ID, OutputPath: a.Generates}
		switch {
		case ctx.Completed[a.ID]:
			st.Status = "done"
		case ready[a.ID]:
			st.Status = "ready"
		default:
			st.Status = "blocked"
			st.MissingDeps = blocked[a.ID]
			if st.MissingDeps == nil {
				st.MissingDeps = []string{}
			}
		}
		byID[a.ID] = st
	}

	var ordered []ArtifactStatus
	for _, id := range ctx.Schema.BuildOrder() {
		ordered = append(ordered, byID[id])
	}

	return &Status{
		ChangeName:    ctx.ChangeName,
		SchemaName:    ctx.SchemaName,
		ChangeRoot:    ctx.ChangeDir,
		ArtifactPaths: paths,
		IsComplete:    ctx.Schema.IsComplete(ctx.Completed),
		ApplyRequires: applyRequires,
		Artifacts:     ordered,
	}
}

func allArtifactIDs(s *schema.Schema) []string {
	ids := make([]string, 0, len(s.Artifacts))
	for _, a := range s.Artifacts {
		ids = append(ids, a.ID)
	}
	return ids
}
