package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveAgentsDirFindsNearestCrewAgentsDirectory(t *testing.T) {
	useDefaultAgentsDirResolverForTest(t)

	rootDir := t.TempDir()
	localCatalog := copyTestAgentsCatalog(t, rootDir)
	nestedDir := filepath.Join(rootDir, "pkg", "feature")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", nestedDir, err)
	}

	t.Chdir(nestedDir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", "")

	resolved, err := resolveAgentsDir()
	if err != nil {
		t.Fatalf("resolveAgentsDir() error = %v", err)
	}
	if resolved != localCatalog {
		t.Fatalf("expected nearest local catalog %q, got %q", localCatalog, resolved)
	}
}

func TestResolveAgentsDirFallsBackToInstalledCatalogWhenNoLocalCatalogExists(t *testing.T) {
	useDefaultAgentsDirResolverForTest(t)

	homeDir := t.TempDir()
	fallbackRoot := filepath.Join(homeDir, ".local", "share", "crew")
	fallbackCatalog := copyTestAgentsCatalog(t, fallbackRoot)

	workDir := t.TempDir()
	t.Chdir(workDir)
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_DATA_HOME", "")

	resolved, err := resolveAgentsDir()
	if err != nil {
		t.Fatalf("resolveAgentsDir() error = %v", err)
	}
	if resolved != fallbackCatalog {
		t.Fatalf("expected installed fallback catalog %q, got %q", fallbackCatalog, resolved)
	}
}

func TestCrewInitCreatesPlaceholderCatalog(t *testing.T) {
	useDefaultAgentsDirResolverForTest(t)

	workDir := t.TempDir()
	t.Chdir(workDir)

	payload := runCLIJSON(t, "init")
	catalogDir := filepath.Join(workDir, localAgentsDirName)
	if payload["initialized"] != true {
		t.Fatalf("expected initialized=true, got %#v", payload)
	}
	if payload["catalog_dir"] != catalogDir {
		t.Fatalf("expected catalog_dir %q, got %#v", catalogDir, payload["catalog_dir"])
	}

	requireTestAgentFile(t, catalogDir, "AGENTS.MD")
	for _, name := range []string{"planner.yaml", "reviewer.yaml", "writer.yaml"} {
		got := mustReadTestAgentFile(t, catalogDir, name)
		want := mustReadTestAgentFile(t, shippedAgentsCatalogDir(t), name)
		if got != want {
			t.Fatalf("expected %s to match shipped crew_agents catalog", name)
		}
	}
}

func TestCrewInitFailsWhenCatalogAlreadyExists(t *testing.T) {
	useDefaultAgentsDirResolverForTest(t)

	workDir := t.TempDir()
	t.Chdir(workDir)
	if err := os.MkdirAll(filepath.Join(workDir, localAgentsDirName), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	var stdout strings.Builder
	var stderr strings.Builder
	exitCode := run(context.Background(), []string{"init"}, &stdout, &stderr, buildInfo{})
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}

	var payload map[string]map[string]string
	if err := json.Unmarshal([]byte(stderr.String()), &payload); err != nil {
		t.Fatalf("stderr was not valid JSON: %v", err)
	}
	if payload["error"]["code"] != "command_failed" {
		t.Fatalf("expected command_failed code, got %q", payload["error"]["code"])
	}
	if !strings.Contains(payload["error"]["message"], localAgentsDirName+" already exists") {
		t.Fatalf("expected existing catalog error, got %q", payload["error"]["message"])
	}
}
