package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"crew/internal/adapters/providers/structuredgeneration"
	"crew/internal/adapters/sandbox"
	"crew/internal/application"
)

type TextConfig struct {
	BinaryPath       string
	WorkingDirectory string
	Timeout          time.Duration
}

type TextProvider struct {
	binaryPath       string
	workingDirectory string
	timeout          time.Duration
}

func NewText(cfg TextConfig) (*TextProvider, error) {
	binaryPath := strings.TrimSpace(cfg.BinaryPath)
	if binaryPath == "" {
		binaryPath = "codex"
	}

	workingDirectory := strings.TrimSpace(cfg.WorkingDirectory)
	if workingDirectory == "" {
		workingDirectory = "."
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	return &TextProvider{
		binaryPath:       binaryPath,
		workingDirectory: workingDirectory,
		timeout:          timeout,
	}, nil
}

func (p *TextProvider) Generate(ctx context.Context, request application.GenerationRequest) (application.GenerationResult, error) {
	if len(request.Messages) == 0 {
		return application.GenerationResult{}, fmt.Errorf("codex generation requires at least one message")
	}
	if strings.TrimSpace(request.Agent.Model) == "" {
		return application.GenerationResult{}, fmt.Errorf("codex generation requires agent model")
	}

	outputDir, err := os.MkdirTemp("", "crew-codex-text-*")
	if err != nil {
		return application.GenerationResult{}, fmt.Errorf("create codex output temp dir: %w", err)
	}
	defer os.RemoveAll(outputDir)

	outputPath := filepath.Join(outputDir, "codex-last-message.txt")
	args := []string{
		"exec",
		"--skip-git-repo-check",
		"--ephemeral",
		"--sandbox", "read-only",
		"--cd", p.workingDirectory,
		"--model", strings.TrimSpace(request.Agent.Model),
		"--output-last-message", outputPath,
		textPrompt(request),
	}

	commandResult, execErr := sandbox.RunCommand(ctx, sandbox.CommandRequest{
		BinaryPath: p.binaryPath,
		Args:       args,
		Dir:        p.workingDirectory,
		Timeout:    p.timeout,
	})

	lastMessage, readErr := os.ReadFile(outputPath)
	if readErr != nil && execErr == nil {
		return application.GenerationResult{}, fmt.Errorf("read codex output: %w", readErr)
	}
	if execErr != nil {
		return application.GenerationResult{}, describeTextExecutionError(execErr, commandResult)
	}

	content := strings.TrimSpace(string(lastMessage))
	if content == "" {
		content = strings.TrimSpace(commandResult.Stdout)
	}
	if content == "" {
		return application.GenerationResult{}, fmt.Errorf("codex response was empty")
	}

	result := structuredgeneration.ParseResult(content)
	result.Metadata = map[string]any{
		"generated_by": "codex_llm",
		"provider":     "codex",
		"model":        strings.TrimSpace(request.Agent.Model),
		"binary_path":  p.binaryPath,
	}
	return result, nil
}

func textPrompt(request application.GenerationRequest) string {
	return structuredgeneration.SystemInstruction(request.Agent) + "\n\n" + structuredgeneration.TranscriptPrompt(request)
}

func describeTextExecutionError(err error, result sandbox.CommandResult) error {
	switch {
	case errors.Is(err, sandbox.ErrExecutionTimeout):
		return fmt.Errorf("codex text generation timed out after %s", result.EndedAt.Sub(result.StartedAt).Round(time.Millisecond))
	case errors.Is(err, sandbox.ErrSetupFailed):
		return err
	default:
		stderr := strings.TrimSpace(result.Stderr)
		if stderr != "" {
			return fmt.Errorf("codex text generation failed: %s", truncate(stderr, 4000))
		}
		return fmt.Errorf("codex text generation failed: %s", truncate(err.Error(), 4000))
	}
}
