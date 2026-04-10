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

func TestRuntimeExecuteTaskStreamsReasoningProgress(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	sandboxRoot := filepath.Join(root, "sandboxes")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(sourceRoot) error = %v", err)
	}

	binaryPath := writeFakeCodexScript(t, root, `#!/bin/sh
OUTPUT=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-last-message)
      OUTPUT="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
printf '{"type":"response.reasoning_summary_text.delta","delta":"Writing"}\n' >&2
printf '{"type":"response.reasoning_summary_text.delta","delta":" the patch"}\n' >&2
printf 'task complete' > "$OUTPUT"
`)

	runtime, err := New(Config{
		BinaryPath:  binaryPath,
		SandboxRoot: sandboxRoot,
		Timeout:     5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var events []application.TransientProgressEvent
	ctx := application.WithTransientProgressReporter(context.Background(), func(event application.TransientProgressEvent) {
		events = append(events, event)
	})
	_, err = runtime.ExecuteTask(ctx, application.SandboxTask{
		ID:                "task-1",
		SessionID:         "session-1",
		ConversationID:    "conversation-1",
		RequestedByAgentID:"writer",
		AssignedAgentID:   "writer",
		AssignedProvider:  application.AgentProviderClassSandboxedRuntime,
		RuntimeName:       "codex",
		WorkspaceRoot:     sourceRoot,
		PermissionProfile: application.SandboxPermissionPatch,
		Instruction:       "update notes",
		Metadata:          map[string]any{"reasoning_effort": "medium"},
		Status:            application.SandboxTaskStatusPending,
		CreatedAt:         time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ExecuteTask() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected streamed progress events, got %+v", events)
	}
	if events[len(events)-1].AgentID != "writer" || events[len(events)-1].Text != "Writing the patch" {
		t.Fatalf("expected writer reasoning progress, got %+v", events)
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

func TestRuntimeExecuteTaskUsesTaskWorkspaceModeOverride(t *testing.T) {
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
printf 'in place' > "$OUTPUT"
printf 'changed in place' > "$WORKDIR/notes.txt"
`)

	runtime, err := New(Config{
		BinaryPath:    binaryPath,
		SandboxRoot:   sandboxRoot,
		WorkspaceMode: sandbox.WorkspaceModeCopied,
		Timeout:       5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := runtime.ExecuteTask(context.Background(), application.SandboxTask{
		ID:                "task-override-mode",
		SessionID:         "session-1",
		ConversationID:    "conversation-1",
		AssignedProvider:  application.AgentProviderClassSandboxedRuntime,
		RuntimeName:       "codex",
		WorkspaceRoot:     sourceRoot,
		PermissionProfile: application.SandboxPermissionPatch,
		Instruction:       "update notes",
		Metadata:          map[string]any{"sandbox_workspace_mode": sandbox.WorkspaceModeInPlace},
		Status:            application.SandboxTaskStatusPending,
		CreatedAt:         time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ExecuteTask() error = %v", err)
	}

	if result.Metadata["workspace_mode"] != sandbox.WorkspaceModeInPlace {
		t.Fatalf("expected in_place workspace metadata, got %+v", result.Metadata)
	}
	content, err := os.ReadFile(filepath.Join(sourceRoot, "notes.txt"))
	if err != nil {
		t.Fatalf("ReadFile(notes.txt) error = %v", err)
	}
	if string(content) != "changed in place" {
		t.Fatalf("expected source workspace to change in place, got %q", string(content))
	}
}

func TestTextProviderGenerateUsesCodexExec(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	promptPath := filepath.Join(root, "captured-prompt.txt")
	modelPath := filepath.Join(root, "captured-model.txt")
	reasoningPath := filepath.Join(root, "captured-reasoning.txt")
	reasoningSummaryPath := filepath.Join(root, "captured-reasoning-summary.txt")
	sandboxModePath := filepath.Join(root, "captured-sandbox.txt")

	binaryPath := writeFakeCodexScript(t, root, "#!/bin/sh\n"+
		"OUTPUT=\"\"\n"+
		"MODEL=\"\"\n"+
		"REASONING=\"\"\n"+
		"REASONING_SUMMARY=\"\"\n"+
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
		"    -c)\n"+
		"      case \"$2\" in\n"+
		"        model_reasoning_effort=*)\n"+
		"          REASONING=\"${2#model_reasoning_effort=}\"\n"+
		"          REASONING=\"${REASONING#\\\"}\"\n"+
		"          REASONING=\"${REASONING%\\\"}\"\n"+
		"          ;;\n"+
		"        model_reasoning_summary=*)\n"+
		"          REASONING_SUMMARY=\"${2#model_reasoning_summary=}\"\n"+
		"          REASONING_SUMMARY=\"${REASONING_SUMMARY#\\\"}\"\n"+
		"          REASONING_SUMMARY=\"${REASONING_SUMMARY%\\\"}\"\n"+
		"          ;;\n"+
		"      esac\n"+
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
		"printf '%s' \"$REASONING\" > "+shellQuote(reasoningPath)+"\n"+
		"printf '%s' \"$REASONING_SUMMARY\" > "+shellQuote(reasoningSummaryPath)+"\n"+
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
	reasoning, err := os.ReadFile(reasoningPath)
	if err != nil {
		t.Fatalf("ReadFile(reasoningPath) error = %v", err)
	}
	if string(reasoning) != agent.ReasoningEffort {
		t.Fatalf("expected reasoning effort %q, got %q", agent.ReasoningEffort, string(reasoning))
	}
	reasoningSummary, err := os.ReadFile(reasoningSummaryPath)
	if err != nil {
		t.Fatalf("ReadFile(reasoningSummaryPath) error = %v", err)
	}
	if string(reasoningSummary) != "" {
		t.Fatalf("expected no reasoning summary config without progress reporting, got %q", string(reasoningSummary))
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

func TestNewTextPreservesZeroTimeout(t *testing.T) {
	t.Parallel()

	provider, err := NewText(TextConfig{BinaryPath: "codex", WorkingDirectory: ".", Timeout: 0})
	if err != nil {
		t.Fatalf("NewText() error = %v", err)
	}
	if provider.timeout != 0 {
		t.Fatalf("expected zero timeout to disable command timeout, got %s", provider.timeout)
	}
}

func TestExtractProgressTextParsesReasoningEvent(t *testing.T) {
	t.Parallel()

	line := `{"type":"reasoning","summary":"Checking the workspace and planning the next reply"}`
	event, ok := extractProgressEvent("planner", line)
	if !ok || event.Text != "Checking the workspace and planning the next reply" || event.Kind != "reasoning" {
		t.Fatalf("unexpected reasoning event %+v ok=%t", event, ok)
	}
}

func TestExtractProgressTextFallsBackToProgressLabel(t *testing.T) {
	t.Parallel()

	line := `{"type":"thinking"}`
	event, ok := extractProgressEvent("planner", line)
	if !ok || event.Text != "thinking" || event.Kind != "thinking" {
		t.Fatalf("unexpected progress event %+v ok=%t", event, ok)
	}
}

func TestExtractProgressEventParsesItemBasedReasoningEvent(t *testing.T) {
	t.Parallel()

	line := `{"event":"item.completed","item":{"type":"reasoning","summary":[{"text":"Checking the workspace"}]}}`
	event, ok := extractProgressEvent("planner", line)
	if !ok || event.Kind != "reasoning" || event.Text != "Checking the workspace" {
		t.Fatalf("unexpected item-based reasoning event %+v ok=%t", event, ok)
	}
}

func TestExtractProgressEventParsesReasoningContentFallback(t *testing.T) {
	t.Parallel()

	line := `{"type":"assistant","reasoning_content":"Tracing the latest response path"}`
	event, ok := extractProgressEvent("planner", line)
	if !ok || event.Text != "Tracing the latest response path" {
		t.Fatalf("unexpected reasoning_content fallback %+v ok=%t", event, ok)
	}
}

func TestExtractProgressEventParsesResponseReasoningSummaryDelta(t *testing.T) {
	t.Parallel()

	line := `{"type":"response.reasoning_summary_text.delta","delta":"Checking the workspace"}`
	event, ok := extractProgressEvent("planner", line)
	if !ok || event.Kind != "reasoning" || event.Text != "Checking the workspace" {
		t.Fatalf("unexpected response reasoning summary delta %+v ok=%t", event, ok)
	}
}

func TestExtractProgressEventParsesSummaryTextField(t *testing.T) {
	t.Parallel()

	line := `{"type":"agent_reasoning","summary_text":"Checking the latest diff"}`
	event, ok := extractProgressEvent("planner", line)
	if !ok || event.Kind != "reasoning" || event.Text != "Checking the latest diff" {
		t.Fatalf("unexpected summary_text reasoning event %+v ok=%t", event, ok)
	}
}

func TestExtractProgressTextIgnoresCompletedEvents(t *testing.T) {
	t.Parallel()

	line := `{"event":"completed","summary":"finalized"}`
	if event, ok := extractProgressEvent("planner", line); ok {
		t.Fatalf("expected completed event to be ignored, got %+v", event)
	}
}

func TestTextProviderGenerateStreamsReasoningFromStderr(t *testing.T) {
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
		"printf '{\"type\":\"reasoning\",\"summary\":\"Checking the workspace\"}\\n' >&2\n"+
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

	var events []application.TransientProgressEvent
	ctx := application.WithTransientProgressReporter(context.Background(), func(event application.TransientProgressEvent) {
		events = append(events, event)
	})
	if _, err := provider.Generate(ctx, application.GenerationRequest{
		Agent: agent,
		Messages: []domain.Message{
			mustMessage("message-1", "review the runtime recovery path"),
		},
	}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if len(events) != 1 || events[0].Text != "Checking the workspace" || events[0].Kind != "reasoning" || events[0].Provider != "codex" {
		t.Fatalf("expected reasoning event from stderr, got %+v", events)
	}
}

func TestTextProviderGenerateRequestsReasoningSummaryWhenProgressEnabled(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	reasoningSummaryPath := filepath.Join(root, "captured-reasoning-summary.txt")
	binaryPath := writeFakeCodexScript(t, root, "#!/bin/sh\n"+
		"OUTPUT=\"\"\n"+
		"REASONING_SUMMARY=\"\"\n"+
		"while [ \"$#\" -gt 0 ]; do\n"+
		"  case \"$1\" in\n"+
		"    --output-last-message)\n"+
		"      OUTPUT=\"$2\"\n"+
		"      shift 2\n"+
		"      ;;\n"+
		"    -c)\n"+
		"      case \"$2\" in\n"+
		"        model_reasoning_summary=*)\n"+
		"          REASONING_SUMMARY=\"${2#model_reasoning_summary=}\"\n"+
		"          REASONING_SUMMARY=\"${REASONING_SUMMARY#\\\"}\"\n"+
		"          REASONING_SUMMARY=\"${REASONING_SUMMARY%\\\"}\"\n"+
		"          ;;\n"+
		"      esac\n"+
		"      shift 2\n"+
		"      ;;\n"+
		"    *)\n"+
		"      shift\n"+
		"      ;;\n"+
		"  esac\n"+
		"done\n"+
		"printf '%s' \"$REASONING_SUMMARY\" > "+shellQuote(reasoningSummaryPath)+"\n"+
		"printf '{\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"Checking\"}\\n' >&2\n"+
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

	ctx := application.WithTransientProgressReporter(context.Background(), func(application.TransientProgressEvent) {})
	if _, err := provider.Generate(ctx, application.GenerationRequest{
		Agent: agent,
		Messages: []domain.Message{
			mustMessage("message-1", "review the runtime recovery path"),
		},
	}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	reasoningSummary, err := os.ReadFile(reasoningSummaryPath)
	if err != nil {
		t.Fatalf("ReadFile(reasoningSummaryPath) error = %v", err)
	}
	if string(reasoningSummary) != "detailed" {
		t.Fatalf("expected reasoning summary config %q, got %q", "detailed", string(reasoningSummary))
	}
}

func TestJSONLProgressSinkAccumulatesReasoningSummaryDeltas(t *testing.T) {
	t.Parallel()

	var events []application.TransientProgressEvent
	sink := newJSONLProgressSink("planner", func(event application.TransientProgressEvent) {
		events = append(events, event)
	})
	if sink == nil {
		t.Fatal("expected progress sink")
	}

	if _, err := sink.Write([]byte("{\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"Checking\"}\n")); err != nil {
		t.Fatalf("Write(first) error = %v", err)
	}
	if _, err := sink.Write([]byte("{\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\" the workspace\"}\n")); err != nil {
		t.Fatalf("Write(second) error = %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 streamed events, got %+v", events)
	}
	if events[0].Text != "Checking" {
		t.Fatalf("expected first delta to render standalone text, got %+v", events[0])
	}
	if events[1].Text != "Checking the workspace" {
		t.Fatalf("expected accumulated delta text, got %+v", events[1])
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
		ID:              "planner",
		Name:            "Planner",
		Role:            "planner",
		SystemPrompt:    "Plan the next step.",
		Provider:        provider,
		Model:           "gpt-5.4",
		ReasoningEffort: "medium",
	})
}

func applicationSandboxAgent(provider string) (domain.Agent, error) {
	return domain.NewAgent(domain.Agent{
		ID:                "planner",
		Name:              "Planner",
		Role:              "planner",
		SystemPrompt:      "Plan the next step.",
		Provider:          provider,
		Model:             "gpt-5.4",
		ReasoningEffort:   "medium",
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
