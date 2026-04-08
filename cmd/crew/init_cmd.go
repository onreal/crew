package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

type catalogTemplateFile struct {
	name    string
	content string
}

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create a local crew_agents catalog in the current directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			workingDir, err := os.Getwd()
			if err != nil {
				return newCLIError("command_failed", fmt.Sprintf("resolve working directory: %v", err))
			}

			catalogDir := filepath.Join(workingDir, localAgentsDirName)
			if _, err := os.Stat(catalogDir); err == nil {
				return newCLIError("command_failed", fmt.Sprintf("%s already exists at %q", localAgentsDirName, catalogDir))
			} else if !os.IsNotExist(err) {
				return newCLIError("command_failed", fmt.Sprintf("stat %s directory %q: %v", localAgentsDirName, catalogDir, err))
			}

			if err := os.MkdirAll(catalogDir, 0o755); err != nil {
				return newCLIError("command_failed", fmt.Sprintf("create %s directory %q: %v", localAgentsDirName, catalogDir, err))
			}

			createdFiles := make([]string, 0, len(initCatalogTemplateFiles()))
			success := false
			defer func() {
				if !success {
					_ = os.RemoveAll(catalogDir)
				}
			}()

			for _, file := range initCatalogTemplateFiles() {
				targetPath := filepath.Join(catalogDir, file.name)
				if err := os.WriteFile(targetPath, []byte(file.content), 0o644); err != nil {
					return newCLIError("command_failed", fmt.Sprintf("write %s file %q: %v", localAgentsDirName, targetPath, err))
				}
				createdFiles = append(createdFiles, targetPath)
			}

			success = true
			return writeJSON(cmd, map[string]any{
				"initialized":   true,
				"catalog_dir":   catalogDir,
				"created_files": createdFiles,
				"agent_count":   3,
			})
		},
	}
}

func initCatalogTemplateFiles() []catalogTemplateFile {
	return []catalogTemplateFile{
		{
			name: "AGENTS.MD",
			content: `# AGENTS.MD

## Purpose

` + "`" + `crew_agents` + "`" + ` is the local workspace catalog of agent definitions for this project.

## Business Role

This directory lets operators keep agent behavior with the workspace they are working on, so ` + "`" + `crew` + "`" + ` can be run from any project directory without depending on one global installed catalog.

## Rules

- each ` + "`" + `.yaml` + "`" + ` or ` + "`" + `.yml` + "`" + ` file defines exactly one agent
- filenames are organizational only; the canonical identity is the agent ` + "`" + `id` + "`" + ` inside the file
- root-level agents are the default catalog for this workspace
- subdirectories may hold additional actor catalogs selected with ` + "`" + `--actors <selector>` + "`" + `
- agent definitions here must stay valid against the runtime agent schema
- duplicate agent IDs across files in the same selected catalog are invalid

## Update Triggers

Update this file when:

- the local agent schema changes
- the workspace agent roster or responsibilities change
- actor subcatalog structure changes
- initialization or discovery behavior for ` + "`" + `crew_agents` + "`" + ` changes
`,
		},
		{
			name: "planner.yaml",
			content: `id: planner
name: Planner
role: planner
system_prompt: Plan the next concrete step from the latest session message.
provider: local_stub
model: gpt-5.4
color: "#fb7185"
tools: []
policies:
  can_initiate: false
  require_direct_mention: false
  allow_broadcast: true
  allow_tool_calls: true
  allow_sandbox_delegation: true
  allowed_sandbox_runtimes:
    - codex
  priority: 100
  weight: 1
  max_consecutive_turns: 1
  max_tool_calls_per_turn: 1
`,
		},
		{
			name: "reviewer.yaml",
			content: `id: reviewer
name: Reviewer
role: reviewer
system_prompt: Review the latest session message and point out the main risk or improvement.
provider: local_stub
model: gpt-5.4
color: "#38bdf8"
tools: []
policies:
  can_initiate: false
  require_direct_mention: false
  allow_broadcast: true
  allow_tool_calls: false
  allow_sandbox_delegation: false
  allowed_sandbox_runtimes: []
  priority: 100
  weight: 1
  max_consecutive_turns: 1
  max_tool_calls_per_turn: 0
`,
		},
		{
			name: "writer.yaml",
			content: `id: writer
name: Writer
role: writer
system_prompt: Draft the next message or action from the latest session context.
provider: local_stub
model: gpt-5.4
color: "#34d399"
tools: []
policies:
  can_initiate: false
  require_direct_mention: false
  allow_broadcast: true
  allow_tool_calls: false
  allow_sandbox_delegation: false
  allowed_sandbox_runtimes: []
  priority: 100
  weight: 1
  max_consecutive_turns: 1
  max_tool_calls_per_turn: 0
`,
		},
	}
}
