package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crew/internal/application"
)

func TestPrepareWorkspaceCopiesSourceAndCapturesChangedArtifacts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	sandboxRoot := filepath.Join(sourceRoot, "var", "sandboxes")

	if err := os.MkdirAll(filepath.Join(sourceRoot, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(sourceRoot, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.git) error = %v", err)
	}
	if err := os.MkdirAll(sandboxRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(sandboxRoot) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "notes.txt"), []byte("original"), 0o644); err != nil {
		t.Fatalf("WriteFile(notes.txt) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, ".git", "config"), []byte("ignored"), 0o644); err != nil {
		t.Fatalf("WriteFile(.git/config) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sandboxRoot, "stale.txt"), []byte("ignore sandbox recursion"), 0o644); err != nil {
		t.Fatalf("WriteFile(stale.txt) error = %v", err)
	}

	workspace, err := PrepareWorkspace(application.AgentTaskID("task-1"), sourceRoot, sandboxRoot)
	if err != nil {
		t.Fatalf("PrepareWorkspace() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(workspace.ExecutionRoot, ".git")); !os.IsNotExist(err) {
		t.Fatalf("expected .git to be excluded from sandbox copy, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace.ExecutionRoot, "var", "sandboxes")); !os.IsNotExist(err) {
		t.Fatalf("expected nested sandbox root to be excluded from sandbox copy, stat err = %v", err)
	}

	if err := os.WriteFile(filepath.Join(workspace.ExecutionRoot, "notes.txt"), []byte("updated"), 0o644); err != nil {
		t.Fatalf("WriteFile(updated notes) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace.ExecutionRoot, "nested", "created.txt"), []byte("created"), 0o644); err != nil {
		t.Fatalf("WriteFile(created.txt) error = %v", err)
	}

	artifacts, err := workspace.ChangedArtifacts()
	if err != nil {
		t.Fatalf("ChangedArtifacts() error = %v", err)
	}

	if len(artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d: %+v", len(artifacts), artifacts)
	}
	if artifacts[0].Path != "nested/created.txt" || artifacts[0].Description != "created" {
		t.Fatalf("unexpected first artifact %+v", artifacts[0])
	}
	if artifacts[1].Path != "notes.txt" || artifacts[1].Description != "modified" {
		t.Fatalf("unexpected second artifact %+v", artifacts[1])
	}

	content, err := os.ReadFile(filepath.Join(sourceRoot, "notes.txt"))
	if err != nil {
		t.Fatalf("ReadFile(source notes) error = %v", err)
	}
	if string(content) != "original" {
		t.Fatalf("expected source workspace to remain unchanged, got %q", string(content))
	}
}

func TestPrepareWorkspaceDoesNotUseRawTaskIDAsFilesystemPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	sandboxRoot := filepath.Join(root, "sandboxes")

	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(sourceRoot) error = %v", err)
	}
	if err := os.MkdirAll(sandboxRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(sandboxRoot) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "notes.txt"), []byte("original"), 0o644); err != nil {
		t.Fatalf("WriteFile(notes.txt) error = %v", err)
	}

	workspace, err := PrepareWorkspace(application.AgentTaskID("../live-repo"), sourceRoot, sandboxRoot)
	if err != nil {
		t.Fatalf("PrepareWorkspace() error = %v", err)
	}

	rel, err := filepath.Rel(sandboxRoot, workspace.TaskRoot)
	if err != nil {
		t.Fatalf("filepath.Rel() error = %v", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("expected task root inside sandbox root, got %q relative path", rel)
	}
	if strings.Contains(workspace.TaskRoot, "..") {
		t.Fatalf("expected sanitized task root, got %q", workspace.TaskRoot)
	}
	if _, err := os.Stat(filepath.Join(sandboxRoot, "..", "live-repo")); !os.IsNotExist(err) {
		t.Fatalf("expected no raw task-id path outside sandbox root, stat err = %v", err)
	}
}

func TestPrepareWorkspaceRejectsSourceSymlinks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	sandboxRoot := filepath.Join(root, "sandboxes")
	targetFile := filepath.Join(root, "outside.txt")

	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(sourceRoot) error = %v", err)
	}
	if err := os.MkdirAll(sandboxRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(sandboxRoot) error = %v", err)
	}
	if err := os.WriteFile(targetFile, []byte("outside"), 0o644); err != nil {
		t.Fatalf("WriteFile(outside.txt) error = %v", err)
	}
	if err := os.Symlink(targetFile, filepath.Join(sourceRoot, "outside-link.txt")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	_, err := PrepareWorkspace(application.AgentTaskID("task-1"), sourceRoot, sandboxRoot)
	if err == nil {
		t.Fatal("expected PrepareWorkspace() to reject source symlink")
	}
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("expected ErrPolicyDenied, got %v", err)
	}
}
