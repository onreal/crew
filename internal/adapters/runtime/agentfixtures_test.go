package runtime

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeDefaultAgentsDir(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}

	sourceDir := filepath.Join(filepath.Dir(file), "..", "..", "..", "agents_test")
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", sourceDir, err)
	}

	targetDir := filepath.Join(t.TempDir(), "agents")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", targetDir, err)
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

func writeSelectorAgentsDir(t *testing.T, rootDir string, selector string) string {
	t.Helper()

	sourceDir := writeDefaultAgentsDir(t)
	targetRoot := filepath.Join(rootDir, "agents")
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", targetRoot, err)
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
		if err := os.WriteFile(filepath.Join(targetRoot, entry.Name()), content, 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", entry.Name(), err)
		}
	}

	targetDir := filepath.Join(targetRoot, selector)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", targetDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(sourceDir, entry.Name()))
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(targetDir, entry.Name()), content, 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", entry.Name(), err)
		}
	}

	return targetDir
}
