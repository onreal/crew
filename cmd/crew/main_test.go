package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesMachineReadableErrorForInvalidMode(t *testing.T) {
	var stdout strings.Builder
	var stderr strings.Builder

	exitCode := run(context.Background(), []string{"session", "start", "--mode", "invalid"}, &stdout, &stderr, buildInfo{})
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}

	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout output, got %q", stdout.String())
	}

	var payload map[string]map[string]string
	if err := json.Unmarshal([]byte(stderr.String()), &payload); err != nil {
		t.Fatalf("stderr was not valid JSON: %v", err)
	}

	if payload["error"]["code"] != "invalid_arguments" {
		t.Fatalf("expected invalid_arguments code, got %q", payload["error"]["code"])
	}
}

func TestRunRejectsUnexpectedPositionalArgs(t *testing.T) {
	testCases := [][]string{
		{"version", "unexpected"},
		{"config", "show", "unexpected"},
		{"session", "start", "unexpected"},
		{"tui", "attach", "unexpected"},
	}

	for _, args := range testCases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var stdout strings.Builder
			var stderr strings.Builder

			exitCode := run(context.Background(), args, &stdout, &stderr, buildInfo{})
			if exitCode != 1 {
				t.Fatalf("expected exit code 1, got %d", exitCode)
			}

			if stdout.Len() != 0 {
				t.Fatalf("expected no stdout output, got %q", stdout.String())
			}

			var payload map[string]map[string]string
			if err := json.Unmarshal([]byte(stderr.String()), &payload); err != nil {
				t.Fatalf("stderr was not valid JSON: %v", err)
			}

			if payload["error"]["code"] != "command_failed" {
				t.Fatalf("expected command_failed code, got %q", payload["error"]["code"])
			}
		})
	}
}

func TestRunUsesLiteralProviderAPIKeyFromConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "crew.yaml")
	storagePath := filepath.Join(dir, "crew.db")

	content := []byte(`
storage:
  path: ` + storagePath + `
providers:
  openai:
    base_url: http://localhost:9999/v1
    api_key: literal-key
    timeout_millis: 30000
    temperature: 0.2
`)
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var stdout strings.Builder
	var stderr strings.Builder
	exitCode := run(context.Background(), []string{"--config", configPath, "session", "start", "--mode", "free"}, &stdout, &stderr, buildInfo{})
	if exitCode != 0 {
		t.Fatalf("expected success with literal api key, got exit code %d and stderr %q", exitCode, stderr.String())
	}
}
