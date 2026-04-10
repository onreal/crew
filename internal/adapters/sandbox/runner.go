package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

type CommandRequest struct {
	BinaryPath string
	Args       []string
	Dir        string
	Env        []string
	Timeout    time.Duration
	StdoutSink io.Writer
	StderrSink io.Writer
}

type CommandResult struct {
	Stdout    string
	Stderr    string
	ExitCode  int
	TimedOut  bool
	StartedAt time.Time
	EndedAt   time.Time
}

func RunCommand(parent context.Context, request CommandRequest) (CommandResult, error) {
	if strings.TrimSpace(request.BinaryPath) == "" {
		return CommandResult{}, fmt.Errorf("%w: binary path must not be empty", ErrSetupFailed)
	}
	if strings.TrimSpace(request.Dir) == "" {
		return CommandResult{}, fmt.Errorf("%w: working directory must not be empty", ErrSetupFailed)
	}

	ctx := parent
	cancel := func() {}
	if request.Timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, request.Timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(ctx, request.BinaryPath, request.Args...)
	cmd.Dir = request.Dir
	cmd.Stdin = strings.NewReader("")
	if len(request.Env) > 0 {
		cmd.Env = append([]string(nil), request.Env...)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	if request.StdoutSink != nil {
		cmd.Stdout = io.MultiWriter(&stdout, request.StdoutSink)
	}
	cmd.Stderr = &stderr
	if request.StderrSink != nil {
		cmd.Stderr = io.MultiWriter(&stderr, request.StderrSink)
	}

	startedAt := time.Now().UTC()
	err := cmd.Run()
	endedAt := time.Now().UTC()

	result := CommandResult{
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		ExitCode:  exitCode(err),
		TimedOut:  errors.Is(ctx.Err(), context.DeadlineExceeded),
		StartedAt: startedAt,
		EndedAt:   endedAt,
	}

	if result.TimedOut {
		return result, fmt.Errorf("%w: %s", ErrExecutionTimeout, strings.TrimSpace(stderr.String()))
	}
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrExecutionFailed, err)
	}

	return result, nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return -1
	}

	return exitErr.ExitCode()
}
