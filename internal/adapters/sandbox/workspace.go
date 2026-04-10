package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"crew/internal/application"
)

type Workspace struct {
	TaskRoot       string
	ExecutionRoot  string
	Mode           string
	initialState   map[string]fileDigest
	ignoredAbsDirs []string
}

const (
	WorkspaceModeCopied  = "copied"
	WorkspaceModeInPlace = "in_place"
)

type fileDigest struct {
	mode fs.FileMode
	size int64
	sha  string
}

func PrepareWorkspace(taskID application.AgentTaskID, sourceRoot, sandboxRoot, workspaceMode string) (Workspace, error) {
	sourceRoot = filepath.Clean(sourceRoot)
	sandboxRoot = filepath.Clean(sandboxRoot)
	workspaceMode = strings.TrimSpace(workspaceMode)
	if workspaceMode == "" {
		workspaceMode = WorkspaceModeCopied
	}

	if strings.TrimSpace(sourceRoot) == "" {
		return Workspace{}, fmt.Errorf("%w: source workspace root must not be empty", ErrSetupFailed)
	}

	absSourceRoot, err := filepath.Abs(sourceRoot)
	if err != nil {
		return Workspace{}, fmt.Errorf("%w: resolve source workspace root: %v", ErrSetupFailed, err)
	}

	info, err := os.Stat(absSourceRoot)
	if err != nil {
		return Workspace{}, fmt.Errorf("%w: stat source workspace root: %v", ErrSetupFailed, err)
	}
	if !info.IsDir() {
		return Workspace{}, fmt.Errorf("%w: source workspace root %q is not a directory", ErrSetupFailed, absSourceRoot)
	}
	if workspaceMode == WorkspaceModeInPlace {
		initialState, err := captureWorkspaceState(absSourceRoot)
		if err != nil {
			return Workspace{}, err
		}
		return Workspace{
			ExecutionRoot: absSourceRoot,
			Mode:          workspaceMode,
			initialState:  initialState,
		}, nil
	}
	if workspaceMode != WorkspaceModeCopied {
		return Workspace{}, fmt.Errorf("%w: unsupported workspace mode %q", ErrSetupFailed, workspaceMode)
	}
	if strings.TrimSpace(sandboxRoot) == "" {
		return Workspace{}, fmt.Errorf("%w: sandbox root must not be empty", ErrSetupFailed)
	}
	absSandboxRoot, err := filepath.Abs(sandboxRoot)
	if err != nil {
		return Workspace{}, fmt.Errorf("%w: resolve sandbox root: %v", ErrSetupFailed, err)
	}

	taskRoot := filepath.Join(absSandboxRoot, sandboxTaskDirName(taskID))
	executionRoot := filepath.Join(taskRoot, "workspace")
	if err := os.RemoveAll(taskRoot); err != nil {
		return Workspace{}, fmt.Errorf("%w: clear sandbox task root: %v", ErrSetupFailed, err)
	}
	if err := os.MkdirAll(executionRoot, 0o755); err != nil {
		return Workspace{}, fmt.Errorf("%w: create execution workspace: %v", ErrSetupFailed, err)
	}

	ignoredAbsDirs := []string{filepath.Join(absSourceRoot, ".git")}
	if containsPath(absSourceRoot, absSandboxRoot) {
		ignoredAbsDirs = append(ignoredAbsDirs, absSandboxRoot)
	}

	if err := copyTree(absSourceRoot, executionRoot, ignoredAbsDirs); err != nil {
		return Workspace{}, err
	}

	initialState, err := captureWorkspaceState(executionRoot)
	if err != nil {
		return Workspace{}, err
	}

	return Workspace{
		TaskRoot:       taskRoot,
		ExecutionRoot:  executionRoot,
		Mode:           workspaceMode,
		initialState:   initialState,
		ignoredAbsDirs: nil,
	}, nil
}

func (w Workspace) ChangedArtifacts() ([]application.SandboxTaskArtifact, error) {
	currentState, err := captureWorkspaceState(w.ExecutionRoot)
	if err != nil {
		return nil, err
	}

	artifacts := make([]application.SandboxTaskArtifact, 0)
	seen := make(map[string]struct{})

	for relPath, before := range w.initialState {
		after, exists := currentState[relPath]
		switch {
		case !exists:
			artifacts = append(artifacts, application.SandboxTaskArtifact{
				Path:        relPath,
				Description: "deleted",
			})
		case after != before:
			artifacts = append(artifacts, application.SandboxTaskArtifact{
				Path:        relPath,
				Description: "modified",
			})
		}
		seen[relPath] = struct{}{}
	}

	for relPath := range currentState {
		if _, exists := seen[relPath]; exists {
			continue
		}
		artifacts = append(artifacts, application.SandboxTaskArtifact{
			Path:        relPath,
			Description: "created",
		})
	}

	sort.Slice(artifacts, func(i, j int) bool {
		return artifacts[i].Path < artifacts[j].Path
	})

	return artifacts, nil
}

func copyTree(sourceRoot, destinationRoot string, ignoredAbsDirs []string) error {
	return filepath.WalkDir(sourceRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("%w: walk source workspace: %v", ErrSetupFailed, walkErr)
		}

		if path == sourceRoot {
			return nil
		}
		for _, ignored := range ignoredAbsDirs {
			if sameOrDescendant(path, ignored) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		relPath, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return fmt.Errorf("%w: compute relative sandbox path: %v", ErrSetupFailed, err)
		}
		targetPath := filepath.Join(destinationRoot, relPath)

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("%w: stat source entry %q: %v", ErrSetupFailed, path, err)
		}

		switch {
		case entry.IsDir():
			return os.MkdirAll(targetPath, info.Mode().Perm())
		case info.Mode()&os.ModeSymlink != 0:
			return fmt.Errorf("%w: source workspace entry %q is a symlink; symlinks are not allowed in copied sandboxes", ErrPolicyDenied, path)
		default:
			return copyFile(path, targetPath, info.Mode().Perm())
		}
	})
}

func copyFile(sourcePath, destinationPath string, mode fs.FileMode) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("%w: open source file %q: %v", ErrSetupFailed, sourcePath, err)
	}
	defer source.Close()

	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
		return fmt.Errorf("%w: create destination directory for %q: %v", ErrSetupFailed, destinationPath, err)
	}

	destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("%w: create destination file %q: %v", ErrSetupFailed, destinationPath, err)
	}
	defer destination.Close()

	if _, err := io.Copy(destination, source); err != nil {
		return fmt.Errorf("%w: copy file %q: %v", ErrSetupFailed, sourcePath, err)
	}

	return nil
}

func captureWorkspaceState(root string) (map[string]fileDigest, error) {
	state := make(map[string]fileDigest)

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("%w: walk execution workspace: %v", ErrSetupFailed, walkErr)
		}
		if entry.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("%w: compute artifact relative path: %v", ErrSetupFailed, err)
		}

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("%w: stat execution file %q: %v", ErrSetupFailed, path, err)
		}

		digest, err := fileState(path, info)
		if err != nil {
			return err
		}
		state[filepath.ToSlash(relPath)] = digest
		return nil
	})
	if err != nil {
		return nil, err
	}

	return state, nil
}

func fileState(path string, info fs.FileInfo) (fileDigest, error) {
	if info.Mode()&os.ModeSymlink != 0 {
		linkTarget, err := os.Readlink(path)
		if err != nil {
			return fileDigest{}, fmt.Errorf("%w: read symlink %q: %v", ErrSetupFailed, path, err)
		}
		return fileDigest{
			mode: info.Mode(),
			size: int64(len(linkTarget)),
			sha:  hashBytes([]byte(linkTarget)),
		}, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return fileDigest{}, fmt.Errorf("%w: open file %q for hashing: %v", ErrSetupFailed, path, err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fileDigest{}, fmt.Errorf("%w: hash file %q: %v", ErrSetupFailed, path, err)
	}

	return fileDigest{
		mode: info.Mode(),
		size: info.Size(),
		sha:  hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func hashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func sandboxTaskDirName(taskID application.AgentTaskID) string {
	return "task-" + hashBytes([]byte(taskID))
}

func containsPath(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && rel != "")
}

func sameOrDescendant(path, parent string) bool {
	if path == parent {
		return true
	}
	rel, err := filepath.Rel(parent, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
