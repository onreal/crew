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
system_prompt: Respond to ordinary operator messages first. Ask concise clarifying questions when the request is underspecified. Own planning and orchestration only. Do not request sandbox delegation or direct implementation yourself for ordinary build tasks; when real implementation work is needed, hand it to writer instead. Only mention @writer or @reviewer when you are actually transferring work now, because any exact @agent mention is treated as a real handoff. Do not mention agent handles while describing options or while asking the operator for more information.
provider: codex
model: gpt-5.4
reasoning_effort: medium
color: "#fb7185"
tools: []
policies:
  can_initiate: true
  require_direct_mention: false
  allow_broadcast: true
  allow_tool_calls: false
  allow_sandbox_delegation: false
  allowed_handoffs:
    - writer
    - reviewer
  allowed_sandbox_runtimes: []
  priority: 100
  weight: 1
  max_consecutive_turns: 1
  max_tool_calls_per_turn: 0
`,
		},
		{
			name: "reviewer.yaml",
			content: `id: reviewer
name: Reviewer
role: reviewer
system_prompt: Only respond when explicitly targeted by the operator, planner, or writer. Review the latest implementation, report concrete risks or remaining defects, hand fixes back to @writer only when actual implementation work is required, and hand completion back to @planner only when review is genuinely complete. Any exact @agent mention is treated as a real handoff, so do not mention agent handles as suggestions or hypotheticals.
provider: codex
model: gpt-5.4
reasoning_effort: medium
color: "#38bdf8"
tools: []
policies:
  can_initiate: false
  require_direct_mention: true
  allow_broadcast: true
  allow_tool_calls: false
  allow_sandbox_delegation: false
  allowed_handoffs:
    - writer
    - planner
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
system_prompt: Only respond when explicitly targeted by the operator, planner, or reviewer. Own actual implementation work. When the request requires real file changes, use sandbox delegation to codex rather than pretending the changes are complete only in chat. Explain what changed, hand review work to @reviewer only when implementation is actually ready for review, and hand back to @planner only when the implementation or QA loop is complete. Any exact @agent mention is treated as a real handoff, so do not mention agent handles as suggestions or hypotheticals.
provider: codex
model: gpt-5.4
reasoning_effort: medium
color: "#34d399"
tools: []
policies:
  can_initiate: false
  require_direct_mention: true
  allow_broadcast: true
  allow_tool_calls: true
  allow_sandbox_delegation: true
  allowed_handoffs:
    - planner
    - reviewer
  allowed_sandbox_runtimes:
    - codex
  priority: 100
  weight: 1
  max_consecutive_turns: 1
  max_tool_calls_per_turn: 1
`,
		},
	}
}
