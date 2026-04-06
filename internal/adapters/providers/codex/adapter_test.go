package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crew/internal/adapters/sandbox"
	"crew/internal/application"
	"crew/internal/domain"
)

func TestRuntimeExecuteTaskCopiesWorkspaceAndCapturesArtifacts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	sandboxRoot := filepath.Join(root, "sandboxes")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(sourceRoot) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "notes.txt"), []byte("original"), 0o644); err != nil {
		t.Fatalf("WriteFile(notes.txt) error = %v", err)
	}

	binaryPath := writeFakeCodexScript(t, root, `#!/bin/sh
OUTPUT=""
WORKDIR=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-last-message)
      OUTPUT="$2"
      shift 2
      ;;
    --cd)
      WORKDIR="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
printf 'codex summary' > "$OUTPUT"
printf 'modified by codex' > "$WORKDIR/notes.txt"
printf 'created by codex' > "$WORKDIR/generated.txt"
printf '{"event":"done"}\n'
`)

	runtime, err := New(Config{
		BinaryPath:  binaryPath,
		SandboxRoot: sandboxRoot,
		Timeout:     5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := runtime.ExecuteTask(context.Background(), application.SandboxTask{
		ID:                "task-1",
		SessionID:         "session-1",
		ConversationID:    "conversation-1",
		AssignedProvider:  application.AgentProviderClassSandboxedRuntime,
		RuntimeName:       "codex",
		WorkspaceRoot:     sourceRoot,
		PermissionProfile: application.SandboxPermissionPatch,
		Instruction:       "update notes",
		Status:            application.SandboxTaskStatusPending,
		CreatedAt:         time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ExecuteTask() error = %v", err)
	}

	if result.Summary != "codex summary" {
		t.Fatalf("expected summary from output file, got %q", result.Summary)
	}
	if len(result.Artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d: %+v", len(result.Artifacts), result.Artifacts)
	}
	if result.Metadata["provider"] != "codex" {
		t.Fatalf("expected provider codex metadata, got %+v", result.Metadata)
	}

	sourceContent, err := os.ReadFile(filepath.Join(sourceRoot, "notes.txt"))
	if err != nil {
		t.Fatalf("ReadFile(source notes) error = %v", err)
	}
	if string(sourceContent) != "original" {
		t.Fatalf("expected source workspace to stay unchanged, got %q", string(sourceContent))
	}
}

func TestRuntimeExecuteTaskReturnsTimeout(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(sourceRoot) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "notes.txt"), []byte("original"), 0o644); err != nil {
		t.Fatalf("WriteFile(notes.txt) error = %v", err)
	}

	binaryPath := writeFakeCodexScript(t, root, `#!/bin/sh
sleep 2
`)

	runtime, err := New(Config{
		BinaryPath:  binaryPath,
		SandboxRoot: filepath.Join(root, "sandboxes"),
		Timeout:     10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := runtime.ExecuteTask(context.Background(), application.SandboxTask{
		ID:                "task-timeout",
		SessionID:         "session-1",
		ConversationID:    "conversation-1",
		AssignedProvider:  application.AgentProviderClassSandboxedRuntime,
		RuntimeName:       "codex",
		WorkspaceRoot:     sourceRoot,
		PermissionProfile: application.SandboxPermissionPatch,
		Instruction:       "sleep",
		Status:            application.SandboxTaskStatusPending,
		CreatedAt:         time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, sandbox.ErrExecutionTimeout) || !strings.Contains(result.ErrorText, "timed out") {
		t.Fatalf("expected timeout execution result, err=%v result=%+v", err, result)
	}
}

func TestRuntimeExecuteTaskUsesTaskSandboxRootOverride(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	defaultSandboxRoot := filepath.Join(root, "default-sandboxes")
	overrideSandboxRoot := filepath.Join(root, "override-sandboxes")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(sourceRoot) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "notes.txt"), []byte("original"), 0o644); err != nil {
		t.Fatalf("WriteFile(notes.txt) error = %v", err)
	}

	binaryPath := writeFakeCodexScript(t, root, `#!/bin/sh
OUTPUT=""
WORKDIR=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-last-message)
      OUTPUT="$2"
      shift 2
      ;;
    --cd)
      WORKDIR="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
printf 'override sandbox root used' > "$OUTPUT"
printf 'changed' > "$WORKDIR/notes.txt"
`)

	runtime, err := New(Config{
		BinaryPath:  binaryPath,
		SandboxRoot: defaultSandboxRoot,
		Timeout:     5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := runtime.ExecuteTask(context.Background(), application.SandboxTask{
		ID:                "task-override",
		SessionID:         "session-1",
		ConversationID:    "conversation-1",
		AssignedProvider:  application.AgentProviderClassSandboxedRuntime,
		RuntimeName:       "codex",
		WorkspaceRoot:     sourceRoot,
		SandboxRoot:       overrideSandboxRoot,
		PermissionProfile: application.SandboxPermissionPatch,
		Instruction:       "update notes",
		Status:            application.SandboxTaskStatusPending,
		CreatedAt:         time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ExecuteTask() error = %v", err)
	}

	if result.Metadata["sandbox_root"] != overrideSandboxRoot {
		t.Fatalf("expected sandbox_root metadata %q, got %+v", overrideSandboxRoot, result.Metadata)
	}
	if _, err := os.Stat(defaultSandboxRoot); !os.IsNotExist(err) {
		t.Fatalf("expected default sandbox root to stay unused, stat err=%v", err)
	}
	if _, err := os.Stat(overrideSandboxRoot); err != nil {
		t.Fatalf("expected override sandbox root to be used, stat err=%v", err)
	}
}

func TestTextProviderGenerateUsesCodexExec(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	promptPath := filepath.Join(root, "captured-prompt.txt")
	modelPath := filepath.Join(root, "captured-model.txt")
	sandboxModePath := filepath.Join(root, "captured-sandbox.txt")

	binaryPath := writeFakeCodexScript(t, root, "#!/bin/sh\n"+
		"OUTPUT=\"\"\n"+
		"MODEL=\"\"\n"+
		"SANDBOX_MODE=\"\"\n"+
		"LAST=\"\"\n"+
		"while [ \"$#\" -gt 0 ]; do\n"+
		"  case \"$1\" in\n"+
		"    --output-last-message)\n"+
		"      OUTPUT=\"$2\"\n"+
		"      shift 2\n"+
		"      ;;\n"+
		"    --model)\n"+
		"      MODEL=\"$2\"\n"+
		"      shift 2\n"+
		"      ;;\n"+
		"    --sandbox)\n"+
		"      SANDBOX_MODE=\"$2\"\n"+
		"      shift 2\n"+
		"      ;;\n"+
		"    *)\n"+
		"      LAST=\"$1\"\n"+
		"      shift\n"+
		"      ;;\n"+
		"  esac\n"+
		"done\n"+
		"printf '%s' \"$MODEL\" > "+shellQuote(modelPath)+"\n"+
		"printf '%s' \"$SANDBOX_MODE\" > "+shellQuote(sandboxModePath)+"\n"+
		"printf '%s' \"$LAST\" > "+shellQuote(promptPath)+"\n"+
		"printf '{\"message_body\":\"Codex reply\",\"sandbox_request\":null}' > \"$OUTPUT\"\n")

	provider, err := NewText(TextConfig{
		BinaryPath:       binaryPath,
		WorkingDirectory: root,
		Timeout:          5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewText() error = %v", err)
	}

	agent, err := applicationTestAgent("codex")
	if err != nil {
		t.Fatalf("applicationTestAgent() error = %v", err)
	}

	result, err := provider.Generate(context.Background(), application.GenerationRequest{
		Agent: agent,
		Messages: []domain.Message{
			mustMessage("message-1", "review the runtime recovery path"),
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if result.MessageBody != "Codex reply" {
		t.Fatalf("expected Codex reply, got %q", result.MessageBody)
	}
	if result.Metadata["provider"] != "codex" {
		t.Fatalf("expected codex provider metadata, got %+v", result.Metadata)
	}

	model, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatalf("ReadFile(modelPath) error = %v", err)
	}
	if string(model) != agent.Model {
		t.Fatalf("expected model %q, got %q", agent.Model, string(model))
	}

	sandboxMode, err := os.ReadFile(sandboxModePath)
	if err != nil {
		t.Fatalf("ReadFile(sandboxModePath) error = %v", err)
	}
	if string(sandboxMode) != "read-only" {
		t.Fatalf("expected read-only sandbox for text generation, got %q", string(sandboxMode))
	}

	prompt, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("ReadFile(promptPath) error = %v", err)
	}
	if !strings.Contains(string(prompt), "review the runtime recovery path") {
		t.Fatalf("expected prompt to include transcript, got %q", string(prompt))
	}
	if !strings.Contains(string(prompt), "\"message_body\"") {
		t.Fatalf("expected prompt to require structured output, got %q", string(prompt))
	}
}

func TestTextProviderGenerateParsesSandboxRequest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	binaryPath := writeFakeCodexScript(t, root, "#!/bin/sh\n"+
		"OUTPUT=\"\"\n"+
		"while [ \"$#\" -gt 0 ]; do\n"+
		"  case \"$1\" in\n"+
		"    --output-last-message)\n"+
		"      OUTPUT=\"$2\"\n"+
		"      shift 2\n"+
		"      ;;\n"+
		"    *)\n"+
		"      shift\n"+
		"      ;;\n"+
		"  esac\n"+
		"done\n"+
		"printf '{\"message_body\":\"Delegating sandbox work\",\"sandbox_request\":{\"instruction\":\"update the README\",\"permission_profile\":\"patch\"}}' > \"$OUTPUT\"\n")

	provider, err := NewText(TextConfig{
		BinaryPath:       binaryPath,
		WorkingDirectory: root,
		Timeout:          5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewText() error = %v", err)
	}

	agent, err := applicationSandboxAgent("codex")
	if err != nil {
		t.Fatalf("applicationSandboxAgent() error = %v", err)
	}

	result, err := provider.Generate(context.Background(), application.GenerationRequest{
		Agent: agent,
		Messages: []domain.Message{
			mustMessage("message-1", "delegate the patch work"),
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if result.SandboxRequest == nil {
		t.Fatal("expected sandbox request")
	}
	if result.SandboxRequest.Instruction != "update the README" {
		t.Fatalf("unexpected sandbox instruction %q", result.SandboxRequest.Instruction)
	}
	if result.SandboxRequest.PermissionProfile != application.SandboxPermissionPatch {
		t.Fatalf("unexpected permission profile %q", result.SandboxRequest.PermissionProfile)
	}
}

func writeFakeCodexScript(t *testing.T, dir, content string) string {
	t.Helper()

	path := filepath.Join(dir, "fake-codex.sh")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(fake codex) error = %v", err)
	}
	return path
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func applicationTestAgent(provider string) (domain.Agent, error) {
	return domain.NewAgent(domain.Agent{
		ID:           "planner",
		Name:         "Planner",
		Role:         "planner",
		SystemPrompt: "Plan the next step.",
		Provider:     provider,
		Model:        "codex-mini",
	})
}

func applicationSandboxAgent(provider string) (domain.Agent, error) {
	return domain.NewAgent(domain.Agent{
		ID:                "planner",
		Name:              "Planner",
		Role:              "planner",
		SystemPrompt:      "Plan the next step.",
		Provider:          provider,
		Model:             "codex-mini",
		DelegationRuntime: "codex",
		Policies: domain.AgentPolicy{
			AllowBroadcast:         true,
			AllowToolCalls:         true,
			AllowSandboxDelegation: true,
			AllowedSandboxRuntimes: []string{"codex"},
			MaxConsecutiveTurns:    2,
			MaxToolCallsPerTurn:    1,
		},
	})
}

func mustMessage(id domain.MessageID, body string) domain.Message {
	message, err := domain.NewMessage(domain.Message{
		ID:             id,
		SessionID:      "session-1",
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           body,
		Timestamp:      time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		panic(err)
	}
	return message
}
