package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	ctx, stop := signalContext(context.Background())
	defer stop()

	exitCode := run(ctx, os.Args[1:], os.Stdout, os.Stderr, buildInfo{
		Version: version,
		Commit:  commit,
		Date:    date,
	})
	os.Exit(exitCode)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, build buildInfo) int {
	return runWithIO(ctx, args, os.Stdin, stdout, stderr, build)
}

func runWithIO(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, build buildInfo) int {
	cmd := newRootCmd(buildInfo{
		Version: build.Version,
		Commit:  build.Commit,
		Date:    build.Date,
	})
	cmd.SetArgs(args)
	cmd.SetIn(stdin)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	if err := cmd.ExecuteContext(ctx); err != nil {
		_ = writeErrorJSON(stderr, classifyCLIError(err))
		return 1
	}

	return 0
}

type cliError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e cliError) Error() string {
	return e.Message
}

func newCLIError(code, message string) error {
	return cliError{
		Code:    code,
		Message: message,
	}
}

func classifyCLIError(err error) cliError {
	var typed cliError
	if ok := asCLIError(err, &typed); ok {
		return typed
	}

	return cliError{
		Code:    "command_failed",
		Message: err.Error(),
	}
}

func asCLIError(err error, target *cliError) bool {
	typed, ok := err.(cliError)
	if ok {
		*target = typed
		return true
	}

	return false
}

func writeErrorJSON(w io.Writer, err cliError) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{
		"error": err,
	})
}
