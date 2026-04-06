package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func setAgentsDirResolverForTest(t *testing.T, dir string) {
	t.Helper()

	previous := agentsDirResolver
	previousIsDefault := agentsDirResolverIsDefault
	agentsDirResolver = func() (string, error) {
		return dir, nil
	}
	agentsDirResolverIsDefault = false
	t.Cleanup(func() {
		agentsDirResolver = previous
		agentsDirResolverIsDefault = previousIsDefault
	})
}

func setSessionStartAutoAttachDetectorForTest(t *testing.T, fn func(in io.Reader, out io.Writer) bool) {
	t.Helper()

	previous := sessionStartAutoAttachDetector
	sessionStartAutoAttachDetector = fn
	t.Cleanup(func() {
		sessionStartAutoAttachDetector = previous
	})
}

func ensureDefaultAgentsDirResolverForTest(t *testing.T) string {
	t.Helper()

	if !agentsDirResolverIsDefault {
		dir, err := agentsDirResolver()
		if err != nil {
			t.Fatalf("agentsDirResolver() error = %v", err)
		}
		return dir
	}

	dir := testAgentsCatalogDir(t)
	setAgentsDirResolverForTest(t, dir)
	return dir
}

func testAgentsCatalogDir(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}

	dir := filepath.Join(filepath.Dir(file), "..", "..", "agents_test")
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", dir, err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %q to be a directory", dir)
	}
	return dir
}

func copyTestAgentsCatalog(t *testing.T, targetRoot string) string {
	t.Helper()

	sourceDir := testAgentsCatalogDir(t)
	targetDir := filepath.Join(targetRoot, "agents")
	return copyAgentsCatalogDir(t, sourceDir, targetDir)
}

func copyTestAgentsCatalogToSelector(t *testing.T, targetRoot, selector string) string {
	t.Helper()

	sourceDir := testAgentsCatalogDir(t)
	targetDir := filepath.Join(targetRoot, "agents", selector)
	return copyAgentsCatalogDir(t, sourceDir, targetDir)
}

func copyAgentsCatalogDir(t *testing.T, sourceDir, targetDir string) string {
	t.Helper()
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", targetDir, err)
	}

	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", sourceDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(sourceDir, entry.Name()))
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", entry.Name(), err)
		}
		targetPath := filepath.Join(targetDir, entry.Name())
		if err := os.WriteFile(targetPath, content, 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", targetPath, err)
		}
	}

	return targetDir
}

func requireTestAgentFile(t *testing.T, dir, name string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat(%q) error = %v", path, err)
	}
	return path
}

func mustReadTestAgentFile(t *testing.T, dir, name string) string {
	t.Helper()

	content, err := os.ReadFile(requireTestAgentFile(t, dir, name))
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", name, err)
	}
	return string(content)
}

func mustWriteTestAgentFile(t *testing.T, dir, name, content string) {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func requirePlannerAgentBody(t *testing.T, dir string) string {
	t.Helper()

	content := mustReadTestAgentFile(t, dir, "planner.yaml")
	if content == "" {
		t.Fatalf("planner.yaml in %q was empty", dir)
	}
	return content
}

func requireTestAgentsCountAtLeast(t *testing.T, dir string, minimum int) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", dir, err)
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		switch filepath.Ext(entry.Name()) {
		case ".yaml", ".yml":
			count++
		}
	}
	if count < minimum {
		t.Fatalf("expected at least %d agent YAML files in %q, got %d", minimum, dir, count)
	}
}

func formatMissingAgentError(name string) string {
	return fmt.Sprintf("agent fixture %q was not found", name)
}
