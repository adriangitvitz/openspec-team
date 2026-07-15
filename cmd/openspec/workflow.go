package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/adriangitvitz/openspec-go/internal/change"
	"github.com/adriangitvitz/openspec-go/internal/core"
	"github.com/adriangitvitz/openspec-go/internal/parser"
)

func resolveRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return core.FindRoot(cwd)
}

func newNewCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "new", Short: "Create new items"}

	var schemaName, description, goal string
	var jsonMode bool
	changeCmd := &cobra.Command{
		Use:   "change <name>",
		Short: "Create a new change directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRoot()
			if err != nil {
				return fail(jsonMode, map[string]any{"change": nil}, "root_not_found", err)
			}
			result, err := change.Create(root, args[0], change.CreateOptions{
				Schema:      schemaName,
				Description: description,
				Goal:        goal,
			})
			if err != nil {
				return fail(jsonMode, map[string]any{"change": nil}, "change_error", err)
			}
			if jsonMode {
				return printJSON(map[string]any{"change": result})
			}
			fmt.Printf("Created change '%s' at %s/\n", result.ID, result.Path)
			fmt.Printf("Schema: %s\n", result.Schema)
			fmt.Printf("Next: openspec status --change %s\n", result.ID)
			return nil
		},
	}
	changeCmd.Flags().StringVar(&schemaName, "schema", "", "Workflow schema to use (default: "+core.DefaultSchema+")")
	changeCmd.Flags().StringVar(&description, "description", "", "Description to add to README.md")
	changeCmd.Flags().StringVar(&goal, "goal", "", "Optional goal metadata to store with the change")
	changeCmd.Flags().BoolVar(&jsonMode, "json", false, "Output as JSON")
	cmd.AddCommand(changeCmd)
	return cmd
}

func newStatusCmd() *cobra.Command {
	var changeName, schemaName string
	var jsonMode bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show artifact completion status for a change",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRoot()
			if err != nil {
				return fail(jsonMode, nil, "root_not_found", err)
			}
			name, err := requireChange(changeName, root)
			if err != nil {
				return fail(jsonMode, nil, "change_not_found", err)
			}
			ctx, err := change.LoadContext(root, name, schemaName)
			if err != nil {
				return fail(jsonMode, nil, "change_error", err)
			}
			captureFingerprints(ctx)
			status := change.BuildStatus(ctx)
			if jsonMode {
				return printJSON(status)
			}
			printStatusText(status)
			return nil
		},
	}
	cmd.Flags().StringVar(&changeName, "change", "", "Change name")
	cmd.Flags().StringVar(&schemaName, "schema", "", "Schema override")
	cmd.Flags().BoolVar(&jsonMode, "json", false, "Output as JSON")
	return cmd
}

func printStatusText(s *change.Status) {
	fmt.Printf("## Status: %s\n", s.ChangeName)
	fmt.Printf("Schema: %s\n\n", s.SchemaName)
	for _, a := range s.Artifacts {
		marker := map[string]string{"done": "✓", "ready": "→", "blocked": "✗"}[a.Status]
		line := fmt.Sprintf("%s %s (%s)", marker, a.ID, a.OutputPath)
		if a.Status == "blocked" && len(a.MissingDeps) > 0 {
			line += " — blocked by: " + strings.Join(a.MissingDeps, ", ")
		}
		fmt.Println(line)
	}
	if s.IsComplete {
		fmt.Println("\nAll artifacts complete.")
	}
}

func newInstructionsCmd() *cobra.Command {
	var changeName, schemaName string
	var jsonMode bool
	cmd := &cobra.Command{
		Use:   "instructions <artifact|apply>",
		Short: "Get enriched instructions for creating an artifact or applying tasks",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRoot()
			if err != nil {
				return fail(jsonMode, nil, "root_not_found", err)
			}
			name, err := requireChange(changeName, root)
			if err != nil {
				return fail(jsonMode, nil, "change_not_found", err)
			}
			ctx, err := change.LoadContext(root, name, schemaName)
			if err != nil {
				return fail(jsonMode, nil, "change_error", err)
			}

			if args[0] == "apply" {
				instructions, err := change.BuildApplyInstructions(ctx)
				if err != nil {
					return fail(jsonMode, nil, "instructions_error", err)
				}
				if jsonMode {
					return printJSON(instructions)
				}
				printApplyText(instructions)
				return nil
			}

			instructions, err := change.BuildInstructions(ctx, args[0])
			if err != nil {
				return fail(jsonMode, nil, "instructions_error", err)
			}
			if jsonMode {
				return printJSON(instructions)
			}
			printInstructionsText(instructions)
			return nil
		},
	}
	cmd.Flags().StringVar(&changeName, "change", "", "Change name")
	cmd.Flags().StringVar(&schemaName, "schema", "", "Schema override")
	cmd.Flags().BoolVar(&jsonMode, "json", false, "Output as JSON")
	return cmd
}

func printInstructionsText(in *change.ArtifactInstructions) {
	fmt.Printf("<artifact id=%q change=%q schema=%q>\n\n", in.ArtifactID, in.ChangeName, in.SchemaName)

	var missing []string
	for _, d := range in.Dependencies {
		if !d.Done {
			missing = append(missing, d.ID)
		}
	}
	if len(missing) > 0 {
		fmt.Println("<warning>")
		fmt.Println("This artifact has unmet dependencies. Complete them first or proceed with caution.")
		fmt.Printf("Missing: %s\n", strings.Join(missing, ", "))
		fmt.Println("</warning>")
		fmt.Println()
	}

	fmt.Println("<task>")
	fmt.Printf("Create the %s artifact for change %q.\n", in.ArtifactID, in.ChangeName)
	fmt.Println(in.Description)
	fmt.Println("</task>")
	fmt.Println()

	if in.Context != "" {
		fmt.Println("<project_context>")
		fmt.Println("<!-- This is background information for you. Do NOT include this in your output. -->")
		fmt.Println(in.Context)
		fmt.Println("</project_context>")
		fmt.Println()
	}
	if len(in.Rules) > 0 {
		fmt.Println("<rules>")
		fmt.Println("<!-- These are constraints for you to follow. Do NOT include this in your output. -->")
		for _, rule := range in.Rules {
			fmt.Printf("- %s\n", rule)
		}
		fmt.Println("</rules>")
		fmt.Println()
	}
	printKnowledgeBlock(in.Knowledge)
	if len(in.Dependencies) > 0 {
		fmt.Println("<dependencies>")
		fmt.Println("Read these files for context before creating this artifact:")
		fmt.Println()
		for _, dep := range in.Dependencies {
			status := "missing"
			if dep.Done {
				status = "done"
			}
			fmt.Printf("<dependency id=%q status=%q>\n", dep.ID, status)
			fmt.Printf("  <path>%s</path>\n", filepath.Join(in.ChangeDir, dep.Path))
			fmt.Printf("  <description>%s</description>\n", dep.Description)
			fmt.Println("</dependency>")
		}
		fmt.Println("</dependencies>")
		fmt.Println()
	}

	fmt.Println("<output>")
	fmt.Printf("Write to: %s\n", in.ResolvedOutputPath)
	fmt.Println("</output>")
	fmt.Println()

	if in.Instruction != "" {
		fmt.Println("<instruction>")
		fmt.Println(strings.TrimSpace(in.Instruction))
		fmt.Println("</instruction>")
		fmt.Println()
	}

	fmt.Println("<template>")
	fmt.Println("<!-- Use this as the structure for your output file. Fill in the sections. -->")
	fmt.Println(strings.TrimSpace(in.Template))
	fmt.Println("</template>")
	fmt.Println()

	if len(in.Unlocks) > 0 {
		fmt.Println("<unlocks>")
		fmt.Printf("Completing this artifact enables: %s\n", strings.Join(in.Unlocks, ", "))
		fmt.Println("</unlocks>")
		fmt.Println()
	}
	fmt.Println("</artifact>")
}

func printApplyText(in *change.ApplyInstructions) {
	fmt.Printf("## Apply: %s\n", in.ChangeName)
	fmt.Printf("Schema: %s\n\n", in.SchemaName)

	if in.State == "blocked" && len(in.MissingArtifacts) > 0 {
		fmt.Println("### Blocked")
		fmt.Println()
		fmt.Printf("Missing artifacts: %s\n\n", strings.Join(in.MissingArtifacts, ", "))
	}
	if len(in.ContextFiles) > 0 {
		fmt.Println("### Context Files")
		for artifactID, files := range in.ContextFiles {
			for _, f := range files {
				fmt.Printf("- %s: %s\n", artifactID, f)
			}
		}
		fmt.Println()
	}
	if len(in.Knowledge) > 0 {
		fmt.Println("### Knowledge Map")
		fmt.Println("Read the entries whose topics relate to this change before implementing:")
		for _, k := range in.Knowledge {
			fmt.Printf("- %s", k.Topic)
			if k.Note != "" {
				fmt.Printf(" — %s", k.Note)
			}
			fmt.Println()
			for _, p := range k.Paths {
				fmt.Printf("    %s\n", p)
			}
		}
		fmt.Println()
	}
	if in.Progress.Total > 0 || len(in.Tasks) > 0 {
		fmt.Println("### Progress")
		suffix := ""
		if in.State == "all_done" {
			suffix = " ✓"
		}
		fmt.Printf("%d/%d complete%s\n\n", in.Progress.Complete, in.Progress.Total, suffix)
	}
	if len(in.Tasks) > 0 {
		fmt.Println("### Tasks")
		for _, t := range in.Tasks {
			checkbox := "[ ]"
			if t.Done {
				checkbox = "[x]"
			}
			fmt.Printf("- %s %s\n", checkbox, t.Description)
		}
		fmt.Println()
	}
	fmt.Println("### Instruction")
	fmt.Println(in.Instruction)
}

func printKnowledgeBlock(entries []core.KnowledgeEntry) {
	if len(entries) == 0 {
		return
	}
	fmt.Println("<knowledge_map>")
	fmt.Println("<!-- Curated index of where domain knowledge lives. Read the files under every topic this change touches BEFORE writing the artifact. -->")
	for _, k := range entries {
		fmt.Printf("<topic name=%q>\n", k.Topic)
		if k.Note != "" {
			fmt.Printf("  <note>%s</note>\n", k.Note)
		}
		for _, p := range k.Paths {
			fmt.Printf("  <path>%s</path>\n", p)
		}
		fmt.Println("</topic>")
	}
	fmt.Println("</knowledge_map>")
	fmt.Println()
}

func newListCmd() *cobra.Command {
	var specs, jsonMode bool
	var sortBy string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active changes or specs",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRoot()
			if err != nil {
				return fail(jsonMode, nil, "root_not_found", err)
			}
			if specs {
				items := change.ListSpecs(root)
				if jsonMode {
					if items == nil {
						items = []change.SpecSummary{}
					}
					return printJSON(map[string]any{"specs": items})
				}
				if len(items) == 0 {
					fmt.Println("No specs found.")
					return nil
				}
				for _, s := range items {
					fmt.Printf("%s  (%d requirements)\n", s.ID, s.RequirementCount)
				}
				return nil
			}
			items := change.ListChanges(root, sortBy)
			if jsonMode {
				if items == nil {
					items = []change.ChangeSummary{}
				}
				return printJSON(map[string]any{"changes": items})
			}
			if len(items) == 0 {
				fmt.Println("No active changes.")
				return nil
			}
			for _, c := range items {
				fmt.Printf("%-30s %s\n", c.Name, parser.FormatTaskStatus(parser.TaskProgress{Total: c.TotalTasks, Completed: c.CompletedTasks}))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&specs, "specs", false, "List specs instead of changes")
	cmd.Flags().StringVar(&sortBy, "sort", "recent", "Sort order: recent|name")
	cmd.Flags().BoolVar(&jsonMode, "json", false, "Output as JSON")
	return cmd
}

func requireChange(name, root string) (string, error) {
	if name != "" {
		return name, nil
	}
	active := change.ActiveChangeNames(root)
	switch len(active) {
	case 0:
		return "", fmt.Errorf("no active changes found. Create one with: openspec new change <name>")
	case 1:
		return active[0], nil
	default:
		return "", fmt.Errorf("multiple active changes; pass --change <name>. Active changes:\n  %s", strings.Join(active, "\n  "))
	}
}
