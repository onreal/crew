package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"crew/internal/adapters/sandbox"
	"crew/internal/application"
	"crew/internal/domain"
)

type Config struct {
	BinaryPath      string
	Model           string
	SandboxRoot     string
	WorkspaceMode   string
	Timeout         time.Duration
	AdditionalWrite []string
}

type Runtime struct {
	binaryPath      string
	model           string
	sandboxRoot     string
	workspaceMode   string
	timeout         time.Duration
	additionalWrite []string
}

func New(cfg Config) (*Runtime, error) {
	binaryPath := strings.TrimSpace(cfg.BinaryPath)
	if binaryPath == "" {
		binaryPath = "codex"
	}
	workspaceMode := strings.TrimSpace(cfg.WorkspaceMode)
	if workspaceMode == "" {
		workspaceMode = sandbox.WorkspaceModeCopied
	}
	if workspaceMode != sandbox.WorkspaceModeCopied && workspaceMode != sandbox.WorkspaceModeInPlace {
		return nil, fmt.Errorf("codex workspace mode must be copied or in_place, got %q", cfg.WorkspaceMode)
	}
	if workspaceMode == sandbox.WorkspaceModeCopied && strings.TrimSpace(cfg.SandboxRoot) == "" {
		return nil, fmt.Errorf("codex sandbox root must not be empty")
	}
	timeout := cfg.Timeout

	return &Runtime{
		binaryPath:      binaryPath,
		model:           strings.TrimSpace(cfg.Model),
		sandboxRoot:     strings.TrimSpace(cfg.SandboxRoot),
		workspaceMode:   workspaceMode,
		timeout:         timeout,
		additionalWrite: append([]string(nil), cfg.AdditionalWrite...),
	}, nil
}

func (r *Runtime) ProviderClass() application.AgentProviderClass {
	return application.AgentProviderClassSandboxedRuntime
}

func (r *Runtime) SupportsRuntime(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), "codex")
}

func (r *Runtime) ExecuteTask(ctx context.Context, task application.SandboxTask) (application.SandboxTaskExecutionResult, error) {
	sandboxRoot := r.sandboxRoot
	if strings.TrimSpace(task.SandboxRoot) != "" {
		sandboxRoot = strings.TrimSpace(task.SandboxRoot)
	}
	workspaceMode := r.workspaceMode
	if override := strings.TrimSpace(metadataString(task.Metadata, "sandbox_workspace_mode")); override != "" {
		workspaceMode = override
	}

	workspace, err := sandbox.PrepareWorkspace(task.ID, task.WorkspaceRoot, sandboxRoot, workspaceMode)
	if err != nil {
		return failedResult(err), err
	}

	outputDir := workspace.TaskRoot
	if strings.TrimSpace(outputDir) == "" {
		outputDir, err = os.MkdirTemp("", "crew-codex-task-*")
		if err != nil {
			return failedResult(err), err
		}
		defer os.RemoveAll(outputDir)
	}
	outputPath := filepath.Join(outputDir, "codex-last-message.txt")
	args := []string{
		"exec",
		"--skip-git-repo-check",
		"--sandbox", codexSandboxMode(task.PermissionProfile),
		"--cd", workspace.ExecutionRoot,
		"--json",
		"--output-last-message", outputPath,
	}
	progressSink := newJSONLProgressSink(taskProgressAgentID(task), application.TransientProgressReporterFromContext(ctx))
	if progressSink != nil {
		args = append(args, "-c", `model_reasoning_summary="detailed"`)
	}
	if effort := strings.TrimSpace(metadataString(task.Metadata, "reasoning_effort")); effort != "" {
		args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=%q", effort))
	}
	if r.model != "" {
		args = append(args, "--model", r.model)
	}
	for _, writableDir := range r.additionalWrite {
		if strings.TrimSpace(writableDir) == "" {
			continue
		}
		args = append(args, "--add-dir", writableDir)
	}
	args = append(args, task.Instruction)

	request := sandbox.CommandRequest{
		BinaryPath: r.binaryPath,
		Args:       args,
		Dir:        workspace.ExecutionRoot,
		Timeout:    r.timeout,
	}
	if progressSink != nil {
		request.StdoutSink = progressSink
		request.StderrSink = progressSink
	}

	commandResult, execErr := sandbox.RunCommand(ctx, request)

	artifacts, artifactErr := workspace.ChangedArtifacts()
	if artifactErr != nil {
		if execErr == nil {
			execErr = artifactErr
		} else {
			execErr = errors.Join(execErr, artifactErr)
		}
	}

	lastMessage, _ := os.ReadFile(outputPath)
	summary := strings.TrimSpace(string(lastMessage))
	if summary == "" {
		summary = truncate(strings.TrimSpace(commandResult.Stdout), 4000)
	}

	result := application.SandboxTaskExecutionResult{
		Summary:   summary,
		Artifacts: artifacts,
		Metadata: map[string]any{
			"provider":            "codex",
			"runtime":             "codex",
			"binary_path":         r.binaryPath,
			"model":               r.model,
			"sandbox_root":        sandboxRoot,
			"workspace_mode":      workspace.Mode,
			"execution_workspace": workspace.ExecutionRoot,
			"command":             append([]string{r.binaryPath}, args...),
			"stdout":              truncate(commandResult.Stdout, 16000),
			"stderr":              truncate(commandResult.Stderr, 16000),
			"exit_code":           commandResult.ExitCode,
			"timed_out":           commandResult.TimedOut,
			"started_at":          commandResult.StartedAt,
			"ended_at":            commandResult.EndedAt,
		},
		CompletedAt: commandResult.EndedAt,
	}

	if execErr != nil {
		result.ErrorText = describeExecutionError(execErr, commandResult)
		return result, execErr
	}

	return result, nil
}

func taskProgressAgentID(task application.SandboxTask) domain.AgentID {
	if task.AssignedAgentID != "" {
		return task.AssignedAgentID
	}
	return task.RequestedByAgentID
}

func codexSandboxMode(profile application.SandboxPermissionProfile) string {
	switch profile {
	case application.SandboxPermissionReadOnly:
		return "read-only"
	case application.SandboxPermissionFullTask:
		return "danger-full-access"
	default:
		return "workspace-write"
	}
}

func describeExecutionError(err error, result sandbox.CommandResult) string {
	switch {
	case errors.Is(err, sandbox.ErrExecutionTimeout):
		return fmt.Sprintf("codex task timed out after %s", result.EndedAt.Sub(result.StartedAt).Round(time.Millisecond))
	case errors.Is(err, sandbox.ErrSetupFailed):
		return err.Error()
	default:
		stderr := strings.TrimSpace(result.Stderr)
		if stderr != "" {
			return truncate(stderr, 4000)
		}
		return truncate(err.Error(), 4000)
	}
}

func failedResult(err error) application.SandboxTaskExecutionResult {
	return application.SandboxTaskExecutionResult{
		ErrorText:   truncate(err.Error(), 4000),
		CompletedAt: time.Now().UTC(),
	}
}

func truncate(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

func metadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}
