package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"

	"github.com/adriangitvitz/openspec-team/internal/agents"
	"github.com/adriangitvitz/openspec-team/internal/core"
	"github.com/adriangitvitz/openspec-team/internal/schema"
)

func targetPath(args []string) (string, error) {
	if len(args) == 1 {
		return filepath.Abs(args[0])
	}
	return os.Getwd()
}

func newInitCmd() *cobra.Command {
	var schemaName string
	cmd := &cobra.Command{
		Use:   "init [path]",
		Short: "Initialize OpenSpec in a project and generate agent integration files (Claude Code, Crush)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := targetPath(args)
			if err != nil {
				return err
			}
			if info, err := os.Stat(root); err != nil || !info.IsDir() {
				return fmt.Errorf("not a directory: %s", root)
			}
			if _, err := schema.Resolve(schemaName, root); err != nil {
				return err
			}

			for _, dir := range []string{
				core.SpecsDir(root),
				core.ArchiveDir(root),
			} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
			}
			configPath := filepath.Join(core.OpenSpecDir(root), "config.yaml")
			knowledgeSeeded := false
			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				hubs := core.DiscoverDocHubs(root)
				knowledgeSeeded = len(hubs) > 0
				content, err := renderInitConfig(schemaName, hubs)
				if err != nil {
					return err
				}
				if err := os.WriteFile(configPath, content, 0o644); err != nil {
					return err
				}
			}

			written, err := agents.Generate(root, version)
			if err != nil {
				return err
			}

			fmt.Printf("Initialized OpenSpec in %s (schema: %s)\n\n", core.OpenSpecDir(root), schemaName)
			if knowledgeSeeded {
				fmt.Println("Seeded openspec/config.yaml with a knowledge map from discovered doc hubs.")
				fmt.Println("Curate it into topical entries — it is injected into every planning instruction.")
				fmt.Println()
			}
			fmt.Println("Generated agent integration files (Claude Code + Crush):")
			for _, path := range written {
				rel, relErr := filepath.Rel(root, path)
				if relErr != nil {
					rel = path
				}
				fmt.Printf("  %s\n", rel)
			}
			fmt.Println("\nNext: talk to your AI —")
			fmt.Println("  /opsx:explore   think through an idea before committing")
			fmt.Println("  /opsx:propose   create a change proposal")
			return nil
		},
	}
	cmd.Flags().StringVar(&schemaName, "schema", core.DefaultSchema,
		"Workflow schema ("+core.DefaultSchema+", spec-driven-deep for complex codebases, or team-driven for the persona workflow)")
	return cmd
}

func renderInitConfig(schemaName string, hubs []core.KnowledgeEntry) ([]byte, error) {
	header := "# OpenSpec project configuration\n" +
		"#\n" +
		"# context:   free-text background injected into every planning instruction\n" +
		"# rules:     per-artifact constraints (map: artifact id -> list of rules)\n" +
		"# knowledge: topics mapped to the files that hold their truth; agents are\n" +
		"#            instructed to read matching entries before planning a change.\n" +
		"#            Validate paths with: openspec validate\n"
	body := struct {
		Schema    string                `yaml:"schema"`
		Knowledge []core.KnowledgeEntry `yaml:"knowledge,omitempty"`
	}{Schema: schemaName, Knowledge: hubs}

	content, err := yaml.Marshal(body)
	if err != nil {
		return nil, err
	}
	return append([]byte(header), content...), nil
}

func newUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update [path]",
		Short: "Refresh generated skill and command files",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			start, err := targetPath(args)
			if err != nil {
				return err
			}
			root, err := core.FindRoot(start)
			if err != nil {
				return err
			}
			written, err := agents.Generate(root, version)
			if err != nil {
				return err
			}
			fmt.Printf("Updated %d generated file(s) in %s\n", len(written), root)
			return nil
		},
	}
}
