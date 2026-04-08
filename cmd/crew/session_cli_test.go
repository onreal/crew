package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sqliteadapter "crew/internal/adapters/storage/sqlite"
	"crew/internal/domain"
)

func TestSessionCommandsPersistAcrossInvocations(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "crew.yaml")
	storagePath := filepath.Join(dir, "crew.db")

	config := []byte("storage:\n  path: " + storagePath + "\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	startPayload := runCLIJSON(t, "--config", configPath, "session", "start", "--mode", "free")
	if startPayload["storage_path"] != storagePath {
		t.Fatalf("expected storage_path %q, got %v", storagePath, startPayload["storage_path"])
	}
	sessionID := startPayload["session"].(map[string]any)["ID"].(string)

	inspectPayload := runCLIJSON(t, "--config", configPath, "session", "inspect", "--session-id", sessionID)
	snapshot := inspectPayload["snapshot"].(map[string]any)
	session := snapshot["Session"].(map[string]any)
	if session["Status"] != "running" {
		t.Fatalf("expected running status after start, got %v", session["Status"])
	}

	stream, ok := snapshot["Stream"].([]any)
	if !ok || len(stream) < 2 {
		t.Fatalf("expected persisted stream entries after separate inspect, got %#v", snapshot["Stream"])
	}

	runCLIJSON(t, "--config", configPath, "session", "pause", "--session-id", sessionID)
	inspectPayload = runCLIJSON(t, "--config", configPath, "session", "inspect", "--session-id", sessionID)
	session = inspectPayload["snapshot"].(map[string]any)["Session"].(map[string]any)
	if session["Status"] != "paused" {
		t.Fatalf("expected paused status, got %v", session["Status"])
	}

	runCLIJSON(t, "--config", configPath, "session", "resume", "--session-id", sessionID)
	runCLIJSON(t, "--config", configPath, "session", "stop", "--session-id", sessionID)
	inspectPayload = runCLIJSON(t, "--config", configPath, "session", "inspect", "--session-id", sessionID)
	session = inspectPayload["snapshot"].(map[string]any)["Session"].(map[string]any)
	if session["Status"] != "stopped" {
		t.Fatalf("expected stopped status, got %v", session["Status"])
	}

	if _, err := os.Stat(storagePath); err != nil {
		t.Fatalf("expected sqlite database at %q: %v", storagePath, err)
	}
}

func TestSessionCommandsRequireSessionID(t *testing.T) {
	var stdout strings.Builder
	var stderr strings.Builder

	exitCode := run(context.Background(), []string{"session", "pause"}, &stdout, &stderr, buildInfo{})
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}

	var payload map[string]map[string]string
	if err := json.Unmarshal([]byte(stderr.String()), &payload); err != nil {
		t.Fatalf("stderr was not valid JSON: %v", err)
	}

	if payload["error"]["code"] != "invalid_arguments" {
		t.Fatalf("expected invalid_arguments code, got %q", payload["error"]["code"])
	}
}

func TestAgentsListReturnsFilesystemCatalog(t *testing.T) {
	agentsDir := ensureDefaultAgentsDirResolverForTest(t)
	payload := runCLIJSON(t, "agents", "list")

	if payload["agents_dir"] != agentsDir {
		t.Fatalf("expected agents_dir %q, got %v", agentsDir, payload["agents_dir"])
	}

	agents := payload["agents"].([]any)
	if len(agents) < 3 {
		t.Fatalf("expected at least 3 default agents, got %d", len(agents))
	}

	foundPlanner := false
	for _, raw := range agents {
		agent := raw.(map[string]any)
		if agent["ID"] == "planner" {
			foundPlanner = true
			break
		}
	}
	if !foundPlanner {
		t.Fatalf("expected planner in agent catalog, got %#v", agents)
	}
}

func TestAgentsListUsesCREWAgentsDirOverride(t *testing.T) {
	rootDir := t.TempDir()
	agentsDir := copyTestAgentsCatalog(t, rootDir)

	t.Setenv(agentsDirEnvVar, agentsDir)

	var stdout strings.Builder
	var stderr strings.Builder
	exitCode := run(context.Background(), []string{"agents", "list"}, &stdout, &stderr, buildInfo{})
	if exitCode != 0 {
		t.Fatalf("expected success, got exit code %d and stderr %s", exitCode, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout.String()), &payload); err != nil {
		t.Fatalf("stdout was not valid JSON: %v\noutput=%s", err, stdout.String())
	}
	if payload["agents_dir"] != agentsDir {
		t.Fatalf("expected env-selected agents_dir %q, got %v", agentsDir, payload["agents_dir"])
	}
}

func TestResolveSelectedAgentsDirUsesSelectorUnderRoot(t *testing.T) {
	rootDir := t.TempDir()
	agentsRoot := filepath.Join(rootDir, localAgentsDirName)
	if err := os.MkdirAll(filepath.Join(agentsRoot, "team-a"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	setAgentsDirResolverForTest(t, agentsRoot)

	selected, err := resolveSelectedAgentsDir("team-a")
	if err != nil {
		t.Fatalf("resolveSelectedAgentsDir() error = %v", err)
	}
	if selected != filepath.Join(agentsRoot, "team-a") {
		t.Fatalf("expected selected dir %q, got %q", filepath.Join(agentsRoot, "team-a"), selected)
	}
}

func TestResolveSelectedAgentsDirRejectsTraversal(t *testing.T) {
	rootDir := t.TempDir()
	agentsRoot := filepath.Join(rootDir, localAgentsDirName)
	if err := os.MkdirAll(agentsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	setAgentsDirResolverForTest(t, agentsRoot)

	if _, err := resolveSelectedAgentsDir("../escape"); err == nil {
		t.Fatal("expected traversal selector to be rejected")
	}
}

func TestAgentsListUsesSelectedActorsCatalog(t *testing.T) {
	rootDir := t.TempDir()
	agentsRoot := copyTestAgentsCatalog(t, rootDir)
	teamDir := copyTestAgentsCatalogToSelector(t, rootDir, "team-a")
	setAgentsDirResolverForTest(t, agentsRoot)

	for _, name := range []string{"planner.yaml", "writer.yaml"} {
		if err := os.Remove(filepath.Join(teamDir, name)); err != nil {
			t.Fatalf("Remove(%q) error = %v", name, err)
		}
	}

	payload := runCLIJSON(t, "--actors", "team-a", "agents", "list")
	if payload["actors"] != "team-a" {
		t.Fatalf("expected actors selector team-a, got %#v", payload["actors"])
	}
	if payload["agents_dir"] != teamDir {
		t.Fatalf("expected selected agents_dir %q, got %v", teamDir, payload["agents_dir"])
	}

	agents := payload["agents"].([]any)
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent in selected catalog, got %#v", agents)
	}
	agent := agents[0].(map[string]any)
	if agent["ID"] != "reviewer" {
		t.Fatalf("expected reviewer-only selected catalog, got %#v", agent)
	}
}

func TestAgentsValidateReturnsValidCatalog(t *testing.T) {
	payload := runCLIJSON(t, "agents", "validate")

	if payload["valid"] != true {
		t.Fatalf("expected valid=true, got %#v", payload)
	}
	if payload["agent_count"].(float64) < 3 {
		t.Fatalf("expected at least 3 agents, got %#v", payload["agent_count"])
	}
}

func TestAgentsSyncPersistsUpdatedYAMLPrompt(t *testing.T) {
	dir := t.TempDir()
	agentsDir := copyTestAgentsCatalog(t, dir)
	setAgentsDirResolverForTest(t, agentsDir)

	storagePath := filepath.Join(dir, "crew.db")
	configPath := filepath.Join(dir, "crew.yaml")
	config := []byte("storage:\n  path: " + storagePath + "\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}

	plannerYAML := strings.Replace(requirePlannerAgentBody(t, agentsDir), "Plan the next concrete step from the latest session message.", "initial planner prompt", 1)
	mustWriteTestAgentFile(t, agentsDir, "planner.yaml", plannerYAML)

	runCLIJSON(t, "--config", configPath, "agents", "sync")

	updatedPlannerYAML := strings.Replace(plannerYAML, "initial planner prompt", "updated planner prompt", 1)
	mustWriteTestAgentFile(t, agentsDir, "planner.yaml", updatedPlannerYAML)

	payload := runCLIJSON(t, "--config", configPath, "agents", "sync")
	if payload["synced"] != true {
		t.Fatalf("expected synced=true, got %#v", payload)
	}
	if payload["agent_count"] != float64(3) {
		t.Fatalf("expected agent_count=3 from shared test catalog, got %#v", payload["agent_count"])
	}

	store, err := sqliteadapter.Open(storagePath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	agent, err := store.Agents().GetByID(context.Background(), domain.AgentID("planner"))
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if agent.SystemPrompt != "updated planner prompt" {
		t.Fatalf("expected persisted prompt to be updated, got %q", agent.SystemPrompt)
	}
}

func TestSessionStepUsesSelectedActorsCatalog(t *testing.T) {
	rootDir := t.TempDir()
	agentsRoot := copyTestAgentsCatalog(t, rootDir)
	teamDir := copyTestAgentsCatalogToSelector(t, rootDir, "team-a")
	setAgentsDirResolverForTest(t, agentsRoot)

	for _, name := range []string{"planner.yaml", "writer.yaml"} {
		if err := os.Remove(filepath.Join(teamDir, name)); err != nil {
			t.Fatalf("Remove(%q) error = %v", name, err)
		}
	}

	storagePath := filepath.Join(rootDir, "crew.db")
	configPath := filepath.Join(rootDir, "crew.yaml")
	config := []byte("storage:\n  path: " + storagePath + "\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}

	startPayload := runCLIJSON(t, "--config", configPath, "--actors", "team-a", "session", "start", "--mode", "free")
	session := startPayload["session"].(map[string]any)
	sessionID := session["ID"].(string)
	if session["ActorCatalog"] != "team-a" {
		t.Fatalf("expected persisted actor catalog team-a, got %#v", session["ActorCatalog"])
	}
	runCLIJSON(t, "--config", configPath, "session", "send", "--session-id", sessionID, "--body", "please reply")

	stepPayload := runCLIJSON(t, "--config", configPath, "session", "step", "--session-id", sessionID)
	step := stepPayload["step"].(map[string]any)
	agent := step["Agent"].(map[string]any)
	if agent["ID"] != "reviewer" {
		t.Fatalf("expected reviewer from persisted session actor catalog, got %#v", agent)
	}
	if ids := step["EligibleAgentIDs"].([]any); len(ids) != 1 || ids[0] != "reviewer" {
		t.Fatalf("expected reviewer-only eligible ids, got %#v", ids)
	}
}

func TestHelpCommandListsAvailableCommands(t *testing.T) {
	payload := runCLIJSON(t, "help")

	commands := payload["commands"].([]any)
	if len(commands) == 0 {
		t.Fatal("expected help command list to be non-empty")
	}

	expected := map[string]bool{
		"crew config sync":     false,
		"crew agents list":     false,
		"crew agents validate": false,
		"crew agents sync":     false,
		"crew session start":   false,
		"crew session inspect": false,
		"crew session tail":    false,
		"crew task create":     false,
		"crew vector rebuild":  false,
		"crew tui attach":      false,
		"crew help":            false,
	}

	for _, raw := range commands {
		entry := raw.(map[string]any)
		command := entry["command"].(string)
		if _, exists := expected[command]; exists {
			expected[command] = true
		}
	}

	for command, found := range expected {
		if !found {
			t.Fatalf("expected help output to include %q, got %#v", command, commands)
		}
	}
}

func TestConfigSyncCopiesActiveConfigToDefaultInstalledPath(t *testing.T) {
	dir := t.TempDir()
	configHome := filepath.Join(dir, "xdg-config")
	sourcePath := filepath.Join(dir, "crew.yaml")
	content := []byte("storage:\n  path: /tmp/source-crew.db\nproviders:\n  grok:\n    base_url: https://api.x.ai/v1\n    api_key: literal-key\n    api_key_env: \"\"\n    timeout_millis: 30000\n    temperature: 0.2\n")
	if err := os.WriteFile(sourcePath, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", configHome)

	payload := runCLIJSON(t, "config", "sync", sourcePath)
	if payload["synced"] != true {
		t.Fatalf("expected synced=true, got %#v", payload)
	}
	targetPath := filepath.Join(configHome, "crew", "crew.yaml")
	if payload["target_config_path"] != targetPath {
		t.Fatalf("expected target path %q, got %#v", targetPath, payload["target_config_path"])
	}

	written, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", targetPath, err)
	}
	if string(written) != string(content) {
		t.Fatalf("expected synced file contents to match source\nsource=%q\ntarget=%q", string(content), string(written))
	}
}

func TestConfigSyncWithoutSourceRequiresActiveYAMLFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))

	var stdout strings.Builder
	var stderr strings.Builder
	exitCode := run(context.Background(), []string{"config", "sync"}, &stdout, &stderr, buildInfo{})
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}

	var payload map[string]map[string]string
	if err := json.Unmarshal([]byte(stderr.String()), &payload); err != nil {
		t.Fatalf("stderr was not valid JSON: %v", err)
	}
	if payload["error"]["code"] != "invalid_configuration" {
		t.Fatalf("expected invalid_configuration code, got %q", payload["error"]["code"])
	}
}

func TestVectorCommandsWorkAcrossInvocations(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "crew.yaml")
	storagePath := filepath.Join(dir, "crew.db")

	config := []byte("storage:\n  path: " + storagePath + "\nvector:\n  dimensions: 8\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	startPayload := runCLIJSON(t, "--config", configPath, "session", "start", "--mode", "free")
	sessionID := startPayload["session"].(map[string]any)["ID"].(string)

	statusPayload := runCLIJSON(t, "--config", configPath, "vector", "status", "--session-id", sessionID)
	if statusPayload["backend_status"] != "disabled" {
		t.Fatalf("expected disabled backend status, got %v", statusPayload["backend_status"])
	}

	rebuildPayload := runCLIJSON(t, "--config", configPath, "vector", "rebuild", "--session-id", sessionID)
	stats := rebuildPayload["stats"].(map[string]any)
	if stats["Scanned"] != float64(0) {
		t.Fatalf("expected 0 scanned messages for empty-session rebuild, got %v", stats["Scanned"])
	}
}

func TestSessionSendRebuildAndRecallAcrossInvocations(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "crew.yaml")
	storagePath := filepath.Join(dir, "crew.db")

	config := []byte("storage:\n  path: " + storagePath + "\nvector:\n  dimensions: 8\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	startPayload := runCLIJSON(t, "--config", configPath, "session", "start", "--mode", "free")
	sessionID := startPayload["session"].(map[string]any)["ID"].(string)

	sendPayload := runCLIJSON(t, "--config", configPath, "session", "send", "--session-id", sessionID, "--body", "review the runtime recovery path")
	message := sendPayload["message"].(map[string]any)
	if message["Body"] != "review the runtime recovery path" {
		t.Fatalf("expected sent body to persist, got %v", message["Body"])
	}

	statusPayload := runCLIJSON(t, "--config", configPath, "vector", "status", "--session-id", sessionID)
	indexState := statusPayload["index_state"].(map[string]any)
	if indexState["Status"] != "stale" {
		t.Fatalf("expected session vector state to be stale after send, got %v", indexState["Status"])
	}

	rebuildPayload := runCLIJSON(t, "--config", configPath, "vector", "rebuild", "--session-id", sessionID)
	stats := rebuildPayload["stats"].(map[string]any)
	if stats["Scanned"] != float64(1) {
		t.Fatalf("expected 1 scanned message for rebuild, got %v", stats["Scanned"])
	}

	recallPayload := runCLIJSON(t, "--config", configPath, "session", "recall", "--session-id", sessionID, "--query", "runtime recovery")
	recall := recallPayload["recall"].(map[string]any)
	if recall["FallbackUsed"] != true {
		t.Fatalf("expected fallback recall in default disabled-vector mode, got %v", recall["FallbackUsed"])
	}

	results := recall["Results"].([]any)
	if len(results) != 1 {
		t.Fatalf("expected 1 recall result, got %d", len(results))
	}

	result := results[0].(map[string]any)
	if result["Strategy"] != "lexical_fallback" {
		t.Fatalf("expected lexical fallback strategy, got %v", result["Strategy"])
	}
	recalledMessage := result["Message"].(map[string]any)
	if recalledMessage["Body"] != "review the runtime recovery path" {
		t.Fatalf("expected recalled body to match sent message, got %v", recalledMessage["Body"])
	}
}

func TestSessionStepPersistsGeneratedReplyAcrossInvocations(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "crew.yaml")
	storagePath := filepath.Join(dir, "crew.db")

	config := []byte("storage:\n  path: " + storagePath + "\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	startPayload := runCLIJSON(t, "--config", configPath, "session", "start", "--mode", "free")
	sessionID := startPayload["session"].(map[string]any)["ID"].(string)

	runCLIJSON(t, "--config", configPath, "session", "send", "--session-id", sessionID, "--body", "plan the next step")
	stepPayload := runCLIJSON(t, "--config", configPath, "session", "step", "--session-id", sessionID)
	step := stepPayload["step"].(map[string]any)
	if step["Stepped"] != true {
		t.Fatalf("expected step to execute, got %v", step["Stepped"])
	}

	inspectPayload := runCLIJSON(t, "--config", configPath, "session", "inspect", "--session-id", sessionID)
	snapshot := inspectPayload["snapshot"].(map[string]any)
	messages := snapshot["Messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages after step, got %d", len(messages))
	}

	last := messages[1].(map[string]any)
	sender := last["Sender"].(map[string]any)
	if sender["Type"] != "agent" {
		t.Fatalf("expected generated agent reply, got sender %v", sender)
	}
	if step["OrchestrationMode"] != "deterministic" {
		t.Fatalf("expected deterministic orchestration mode, got %v", step["OrchestrationMode"])
	}
	eligible := step["EligibleAgentIDs"].([]any)
	if len(eligible) < 1 {
		t.Fatalf("expected eligible agent diagnostics, got %#v", step)
	}
	ordered := step["OrderedCandidateIDs"].([]any)
	if len(ordered) < len(eligible) {
		t.Fatalf("expected ordered candidate diagnostics, got %#v", step)
	}
}

func TestSessionSendSupportsDirectRecipientRouting(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "crew.yaml")
	storagePath := filepath.Join(dir, "crew.db")

	config := []byte("storage:\n  path: " + storagePath + "\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	startPayload := runCLIJSON(t, "--config", configPath, "session", "start", "--mode", "free")
	sessionID := startPayload["session"].(map[string]any)["ID"].(string)

	sendPayload := runCLIJSON(
		t,
		"--config", configPath,
		"session", "send",
		"--session-id", sessionID,
		"--to-agent", "reviewer",
		"--to-agent", "writer",
		"--body", "review and draft this response",
	)
	message := sendPayload["message"].(map[string]any)
	if message["Channel"] != "direct" {
		t.Fatalf("expected direct channel, got %v", message["Channel"])
	}
	recipients := message["ToAgentIDs"].([]any)
	if len(recipients) != 2 || recipients[0] != "reviewer" || recipients[1] != "writer" {
		t.Fatalf("unexpected direct recipients %#v", recipients)
	}
}

func TestSessionSendSupportsReplyTo(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "crew.yaml")
	storagePath := filepath.Join(dir, "crew.db")

	config := []byte("storage:\n  path: " + storagePath + "\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	startPayload := runCLIJSON(t, "--config", configPath, "session", "start", "--mode", "free")
	sessionID := startPayload["session"].(map[string]any)["ID"].(string)

	firstPayload := runCLIJSON(
		t,
		"--config", configPath,
		"session", "send",
		"--session-id", sessionID,
		"--body", "initial thread message",
	)
	firstID := firstPayload["message"].(map[string]any)["ID"].(string)

	replyPayload := runCLIJSON(
		t,
		"--config", configPath,
		"session", "send",
		"--session-id", sessionID,
		"--reply-to", firstID,
		"--body", "follow-up thread message",
	)
	message := replyPayload["message"].(map[string]any)
	if message["ReplyTo"] != firstID {
		t.Fatalf("expected reply_to %q, got %v", firstID, message["ReplyTo"])
	}
}

func TestSessionTailSupportsConversationFiltering(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "crew.yaml")
	storagePath := filepath.Join(dir, "crew.db")

	config := []byte("storage:\n  path: " + storagePath + "\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	startPayload := runCLIJSON(t, "--config", configPath, "session", "start", "--mode", "free")
	sessionID := startPayload["session"].(map[string]any)["ID"].(string)

	runCLIJSON(t, "--config", configPath, "session", "send", "--session-id", sessionID, "--conversation-id", "conversation-a", "--body", "alpha thread")
	runCLIJSON(t, "--config", configPath, "session", "send", "--session-id", sessionID, "--conversation-id", "conversation-b", "--body", "beta thread")

	output := runCLIText(t, "--config", configPath, "session", "tail", "--session-id", sessionID, "--conversation-id", "conversation-a")
	if !strings.Contains(output, "alpha thread") {
		t.Fatalf("expected tail output to include conversation-a message, got %q", output)
	}
	if strings.Contains(output, "beta thread") {
		t.Fatalf("expected tail output to exclude conversation-b message, got %q", output)
	}
}

func TestTUIAttachPrintsLiveSessionView(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "crew.yaml")
	storagePath := filepath.Join(dir, "crew.db")

	config := []byte("storage:\n  path: " + storagePath + "\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	startPayload := runCLIJSON(t, "--config", configPath, "session", "start", "--mode", "free")
	sessionID := startPayload["session"].(map[string]any)["ID"].(string)
	runCLIJSON(t, "--config", configPath, "session", "send", "--session-id", sessionID, "--body", "shared room message")

	output := runCLIText(t, "--config", configPath, "tui", "attach", "--session-id", sessionID, "--follow=false")
	if !strings.Contains(output, "attached to session "+sessionID) {
		t.Fatalf("expected attach header, got %q", output)
	}
	if !strings.Contains(output, "shared room message") {
		t.Fatalf("expected attach output to include session message, got %q", output)
	}
}

func TestSessionStartFreeAutoAttachesImmediately(t *testing.T) {
	rootDir := t.TempDir()
	agentsRoot := copyTestAgentsCatalog(t, rootDir)
	teamDir := copyTestAgentsCatalogToSelector(t, rootDir, "team-a")
	setAgentsDirResolverForTest(t, agentsRoot)
	setSessionStartAutoAttachDetectorForTest(t, func(in io.Reader, out io.Writer) bool {
		return true
	})

	for _, name := range []string{"planner.yaml", "writer.yaml"} {
		if err := os.Remove(filepath.Join(teamDir, name)); err != nil {
			t.Fatalf("Remove(%q) error = %v", name, err)
		}
	}

	configPath := filepath.Join(rootDir, "crew.yaml")
	storagePath := filepath.Join(rootDir, "crew.db")
	config := []byte("storage:\n  path: " + storagePath + "\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	output := runCLITextInput(
		t,
		"please reply\n/quit\n",
		"--config", configPath,
		"--actors", "team-a",
		"session", "start",
		"--mode", "free",
	)
	if !strings.Contains(output, "attached to session session-1") {
		t.Fatalf("expected auto-attach header, got %q", output)
	}
	if !strings.Contains(output, "reviewer (reply_to=") {
		t.Fatalf("expected attached room to use persisted selected actors catalog, got %q", output)
	}
	if strings.Contains(output, "planner (") {
		t.Fatalf("expected planner to be absent from selected actors catalog, got %q", output)
	}

	inspectPayload := runCLIJSON(t, "--config", configPath, "session", "inspect", "--session-id", "session-1")
	session := inspectPayload["snapshot"].(map[string]any)["Session"].(map[string]any)
	if session["ActorCatalog"] != "team-a" {
		t.Fatalf("expected persisted actor catalog team-a, got %#v", session["ActorCatalog"])
	}
}

func TestTUIAttachAcceptsInteractiveInput(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "crew.yaml")
	storagePath := filepath.Join(dir, "crew.db")

	config := []byte("storage:\n  path: " + storagePath + "\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	startPayload := runCLIJSON(t, "--config", configPath, "session", "start", "--mode", "free")
	sessionID := startPayload["session"].(map[string]any)["ID"].(string)

	output := runCLITextInput(
		t,
		"plan the next step\n/quit\n",
		"--config", configPath,
		"tui", "attach",
		"--session-id", sessionID,
		"--conversation-id", "conversation-1",
		"--poll-interval-millis", "1",
		"--auto-steps", "3",
		"--orchestration", "round_robin",
		"--reply-routing", "latest_speaker",
	)
	if !strings.Contains(output, "plan the next step") {
		t.Fatalf("expected attach output to include operator message, got %q", output)
	}
	if !strings.Contains(output, "auto_steps=3 orchestration=round_robin") {
		t.Fatalf("expected attach header to include interactive auto-run settings, got %q", output)
	}
	if !strings.Contains(output, "Planner (planner): plan the next step") {
		t.Fatalf("expected attach output to include planner reply, got %q", output)
	}
	agentReplies := 0
	for _, marker := range []string{
		"planner (reply_to=",
		"reviewer (to=planner reply_to=",
		"planner (to=reviewer reply_to=",
	} {
		agentReplies += strings.Count(output, marker)
	}
	if agentReplies < 3 {
		t.Fatalf("expected plain text attach input to auto-run multiple agent turns, got %q", output)
	}
	if !strings.Contains(output, "reviewer (to=planner reply_to=") {
		t.Fatalf("expected latest-speaker routing to direct reviewer toward planner, got %q", output)
	}
	if !strings.Contains(output, "planner (to=reviewer reply_to=") {
		t.Fatalf("expected latest-speaker routing to direct planner back toward reviewer, got %q", output)
	}
}

func TestSessionStartSequentialRemainsJSONWhenAutoAttachWouldBeAvailable(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "crew.yaml")
	storagePath := filepath.Join(dir, "crew.db")
	setSessionStartAutoAttachDetectorForTest(t, func(in io.Reader, out io.Writer) bool {
		return true
	})

	config := []byte("storage:\n  path: " + storagePath + "\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	output := runCLITextInput(
		t,
		"/quit\n",
		"--config", configPath,
		"session", "start",
		"--mode", "sequential",
	)

	var payload map[string]any
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("expected sequential session start to remain JSON, got %q: %v", output, err)
	}
	session := payload["session"].(map[string]any)
	if session["Mode"] != "sequential" {
		t.Fatalf("expected sequential session JSON payload, got %#v", session)
	}
}

func TestSessionStepSupportsMentionedFirstOrchestration(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "crew.yaml")
	storagePath := filepath.Join(dir, "crew.db")

	config := []byte("storage:\n  path: " + storagePath + "\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	startPayload := runCLIJSON(t, "--config", configPath, "session", "start", "--mode", "free")
	sessionID := startPayload["session"].(map[string]any)["ID"].(string)

	runCLIJSON(t, "--config", configPath, "session", "send", "--session-id", sessionID, "--body", "writer please draft the response")
	stepPayload := runCLIJSON(t, "--config", configPath, "session", "step", "--session-id", sessionID, "--orchestration", "mentioned_first")
	step := stepPayload["step"].(map[string]any)
	agent := step["Agent"].(map[string]any)
	if agent["ID"] != "writer" {
		t.Fatalf("expected writer to be selected by mentioned_first, got %v", agent["ID"])
	}
	if step["OrchestrationMode"] != "mentioned_first" {
		t.Fatalf("expected mentioned_first mode, got %v", step["OrchestrationMode"])
	}
	order := step["OrderedCandidateIDs"].([]any)
	if len(order) == 0 || order[0] != "writer" {
		t.Fatalf("expected writer to lead candidate order, got %#v", order)
	}
}

func TestSessionStepDelegatesSandboxTaskAcrossInvocations(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "crew.yaml")
	storagePath := filepath.Join(dir, "crew.db")
	sandboxRoot := filepath.Join(dir, "sandboxes")
	sourceRoot := filepath.Join(dir, "source")

	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(sourceRoot) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "notes.txt"), []byte("original"), 0o600); err != nil {
		t.Fatalf("WriteFile(notes.txt) error = %v", err)
	}

	fakeCodex := filepath.Join(dir, "fake-codex.sh")
	script := `#!/bin/sh
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
printf 'delegated sandbox result' > "$OUTPUT"
printf 'updated from delegated codex' > "$WORKDIR/notes.txt"
`
	if err := os.WriteFile(fakeCodex, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(fakeCodex) error = %v", err)
	}

	config := []byte(
		"storage:\n  path: " + storagePath + "\n" +
			"sandbox:\n" +
			"  provider: codex\n" +
			"  binary: " + fakeCodex + "\n" +
			"  source_workspace_root: " + sourceRoot + "\n" +
			"  workspace_root: " + sandboxRoot + "\n" +
			"  permission_profile: patch\n",
	)
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}

	startPayload := runCLIJSON(t, "--config", configPath, "session", "start", "--mode", "free")
	sessionID := startPayload["session"].(map[string]any)["ID"].(string)

	runCLIJSON(t, "--config", configPath, "session", "send", "--session-id", sessionID, "--body", "sandbox: update notes")
	stepPayload := runCLIJSON(t, "--config", configPath, "session", "step", "--session-id", sessionID)
	step := stepPayload["step"].(map[string]any)
	if step["SandboxTask"] == nil {
		t.Fatalf("expected sandbox task in step payload, got %#v", step)
	}

	task := step["SandboxTask"].(map[string]any)
	if task["Status"] != "succeeded" {
		t.Fatalf("expected succeeded sandbox task, got %v", task["Status"])
	}
	taskMessages := step["TaskMessages"].([]any)
	if len(taskMessages) != 2 {
		t.Fatalf("expected 2 task messages, got %d", len(taskMessages))
	}

	listPayload := runCLIJSON(t, "--config", configPath, "task", "list", "--session-id", sessionID)
	tasks := listPayload["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 persisted sandbox task, got %d", len(tasks))
	}

	inspectPayload := runCLIJSON(t, "--config", configPath, "session", "inspect", "--session-id", sessionID)
	messages := inspectPayload["snapshot"].(map[string]any)["Messages"].([]any)
	if len(messages) != 4 {
		t.Fatalf("expected user + agent + 2 sandbox messages, got %d", len(messages))
	}
}

func TestSessionStepScopesToRequestedConversationAcrossInvocations(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "crew.yaml")
	storagePath := filepath.Join(dir, "crew.db")

	config := []byte("storage:\n  path: " + storagePath + "\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	startPayload := runCLIJSON(t, "--config", configPath, "session", "start", "--mode", "free")
	sessionID := startPayload["session"].(map[string]any)["ID"].(string)

	runCLIJSON(t, "--config", configPath, "session", "send", "--session-id", sessionID, "--conversation-id", "conversation-a", "--body", "first thread context")
	runCLIJSON(t, "--config", configPath, "session", "send", "--session-id", sessionID, "--conversation-id", "conversation-a", "--body", "target thread latest")
	runCLIJSON(t, "--config", configPath, "session", "send", "--session-id", sessionID, "--conversation-id", "conversation-b", "--body", "newer unrelated thread")

	stepPayload := runCLIJSON(t, "--config", configPath, "session", "step", "--session-id", sessionID, "--conversation-id", "conversation-a")
	step := stepPayload["step"].(map[string]any)
	if step["ConversationID"] != "conversation-a" {
		t.Fatalf("expected step conversation-a, got %v", step["ConversationID"])
	}

	message := step["Message"].(map[string]any)
	if message["ConversationID"] != "conversation-a" {
		t.Fatalf("expected generated message in conversation-a, got %v", message["ConversationID"])
	}
	if message["Body"] != "Planner (planner): target thread latest" {
		t.Fatalf("expected generated reply to use conversation-a context, got %v", message["Body"])
	}
	if message["ReplyTo"] == "" {
		t.Fatal("expected generated reply to target the latest conversation-a message")
	}
}

func TestSessionAutoPersistsGeneratedRepliesAcrossInvocations(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "crew.yaml")
	storagePath := filepath.Join(dir, "crew.db")

	config := []byte("storage:\n  path: " + storagePath + "\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	startPayload := runCLIJSON(t, "--config", configPath, "session", "start", "--mode", "free")
	sessionID := startPayload["session"].(map[string]any)["ID"].(string)

	runCLIJSON(t, "--config", configPath, "session", "send", "--session-id", sessionID, "--body", "plan the next steps")
	autoPayload := runCLIJSON(t, "--config", configPath, "session", "auto", "--session-id", sessionID, "--max-steps", "2")
	auto := autoPayload["auto"].(map[string]any)
	if auto["CompletedSteps"] != float64(2) {
		t.Fatalf("expected 2 completed steps, got %v", auto["CompletedSteps"])
	}
	if auto["StopReason"] != "max_steps_reached" {
		t.Fatalf("expected max_steps_reached, got %v", auto["StopReason"])
	}
	if auto["VectorStateMarkedStale"] != true {
		t.Fatalf("expected vector state to be marked stale, got %v", auto["VectorStateMarkedStale"])
	}

	steps := auto["Steps"].([]any)
	if len(steps) != 2 {
		t.Fatalf("expected 2 step entries, got %d", len(steps))
	}

	inspectPayload := runCLIJSON(t, "--config", configPath, "session", "inspect", "--session-id", sessionID)
	snapshot := inspectPayload["snapshot"].(map[string]any)
	messages := snapshot["Messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages after auto run, got %d", len(messages))
	}
}

func TestSessionSendRequiresBody(t *testing.T) {
	var stdout strings.Builder
	var stderr strings.Builder

	exitCode := run(context.Background(), []string{"session", "send", "--session-id", "session-1"}, &stdout, &stderr, buildInfo{})
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}

	var payload map[string]map[string]string
	if err := json.Unmarshal([]byte(stderr.String()), &payload); err != nil {
		t.Fatalf("stderr was not valid JSON: %v", err)
	}

	if payload["error"]["code"] != "invalid_arguments" {
		t.Fatalf("expected invalid_arguments code, got %q", payload["error"]["code"])
	}
}

func TestSessionRecallRequiresQuery(t *testing.T) {
	var stdout strings.Builder
	var stderr strings.Builder

	exitCode := run(context.Background(), []string{"session", "recall", "--session-id", "session-1"}, &stdout, &stderr, buildInfo{})
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}

	var payload map[string]map[string]string
	if err := json.Unmarshal([]byte(stderr.String()), &payload); err != nil {
		t.Fatalf("stderr was not valid JSON: %v", err)
	}

	if payload["error"]["code"] != "invalid_arguments" {
		t.Fatalf("expected invalid_arguments code, got %q", payload["error"]["code"])
	}
}

func TestSessionAutoRequiresPositiveMaxSteps(t *testing.T) {
	var stdout strings.Builder
	var stderr strings.Builder

	exitCode := run(context.Background(), []string{"session", "auto", "--session-id", "session-1"}, &stdout, &stderr, buildInfo{})
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}

	var payload map[string]map[string]string
	if err := json.Unmarshal([]byte(stderr.String()), &payload); err != nil {
		t.Fatalf("stderr was not valid JSON: %v", err)
	}

	if payload["error"]["code"] != "invalid_arguments" {
		t.Fatalf("expected invalid_arguments code, got %q", payload["error"]["code"])
	}
}

func TestTaskCreateRunAndListAcrossInvocations(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "crew.yaml")
	storagePath := filepath.Join(dir, "crew.db")
	sandboxRoot := filepath.Join(dir, "sandboxes")
	sourceRoot := filepath.Join(dir, "source")

	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(sourceRoot) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "notes.txt"), []byte("original"), 0o600); err != nil {
		t.Fatalf("WriteFile(notes.txt) error = %v", err)
	}

	fakeCodex := filepath.Join(dir, "fake-codex.sh")
	script := `#!/bin/sh
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
printf 'sandboxed codex result' > "$OUTPUT"
printf 'updated from codex' > "$WORKDIR/notes.txt"
printf 'new file' > "$WORKDIR/generated.txt"
`
	if err := os.WriteFile(fakeCodex, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(fakeCodex) error = %v", err)
	}

	config := []byte("storage:\n  path: " + storagePath + "\n" +
		"sandbox:\n" +
		"  provider: codex\n" +
		"  binary: " + fakeCodex + "\n" +
		"  workspace_root: " + sandboxRoot + "\n" +
		"  permission_profile: patch\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}

	startPayload := runCLIJSON(t, "--config", configPath, "session", "start", "--mode", "free")
	sessionID := startPayload["session"].(map[string]any)["ID"].(string)

	createPayload := runCLIJSON(
		t,
		"--config", configPath,
		"task", "create",
		"--session-id", sessionID,
		"--instruction", "update notes",
		"--workspace-root", sourceRoot,
	)
	task := createPayload["task"].(map[string]any)
	taskID := task["ID"].(string)
	if task["Status"] != "pending" {
		t.Fatalf("expected pending task, got %v", task["Status"])
	}

	runPayload := runCLIJSON(t, "--config", configPath, "task", "run", "--task-id", taskID)
	task = runPayload["task"].(map[string]any)
	if task["Status"] != "succeeded" {
		t.Fatalf("expected succeeded task, got %v", task["Status"])
	}
	if task["ResultSummary"] != "sandboxed codex result" {
		t.Fatalf("expected result summary from fake codex, got %v", task["ResultSummary"])
	}

	artifacts := task["Artifacts"].([]any)
	if len(artifacts) != 2 {
		t.Fatalf("expected 2 task artifacts, got %d", len(artifacts))
	}

	listPayload := runCLIJSON(t, "--config", configPath, "task", "list", "--session-id", sessionID)
	tasks := listPayload["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	inspectPayload := runCLIJSON(t, "--config", configPath, "session", "inspect", "--session-id", sessionID)
	stream := inspectPayload["snapshot"].(map[string]any)["Stream"].([]any)
	foundTaskUpdate := false
	for _, entry := range stream {
		if entry.(map[string]any)["Topic"] == "agent_task.updated" {
			foundTaskUpdate = true
			break
		}
	}
	if !foundTaskUpdate {
		t.Fatalf("expected session stream to include agent_task.updated, got %#v", stream)
	}

	sourceContent, err := os.ReadFile(filepath.Join(sourceRoot, "notes.txt"))
	if err != nil {
		t.Fatalf("ReadFile(source notes) error = %v", err)
	}
	if string(sourceContent) != "original" {
		t.Fatalf("expected source workspace to remain unchanged, got %q", string(sourceContent))
	}
}

func TestSessionStepFailsWhenSelectedAgentProviderHasNoAPIKey(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "crew.yaml")
	storagePath := filepath.Join(dir, "crew.db")
	agentsDir := copyTestAgentsCatalog(t, dir)
	setAgentsDirResolverForTest(t, agentsDir)

	plannerYAML := strings.Replace(requirePlannerAgentBody(t, agentsDir), "provider: local_stub", "provider: openai", 1)
	mustWriteTestAgentFile(t, agentsDir, "planner.yaml", plannerYAML)

	config := []byte("storage:\n  path: " + storagePath + "\nproviders:\n  openai:\n    base_url: http://localhost:8080/v1\n    api_key_env: MISSING_TEST_OPENAI_API_KEY\n    timeout_millis: 30000\n    temperature: 0.2\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var stdout strings.Builder
	var stderr strings.Builder
	exitCode := run(context.Background(), []string{"--config", configPath, "session", "start", "--mode", "free"}, &stdout, &stderr, buildInfo{})
	if exitCode != 0 {
		t.Fatalf("expected session start to succeed, got %d with stderr %q", exitCode, stderr.String())
	}

	var startPayload map[string]any
	if err := json.Unmarshal([]byte(stdout.String()), &startPayload); err != nil {
		t.Fatalf("stdout was not valid JSON: %v", err)
	}
	sessionID := startPayload["session"].(map[string]any)["ID"].(string)

	stdout.Reset()
	stderr.Reset()
	exitCode = run(context.Background(), []string{"--config", configPath, "session", "send", "--session-id", sessionID, "--body", "please plan this"}, &stdout, &stderr, buildInfo{})
	if exitCode != 0 {
		t.Fatalf("expected session send to succeed, got %d with stderr %q", exitCode, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = run(context.Background(), []string{"--config", configPath, "session", "step", "--session-id", sessionID}, &stdout, &stderr, buildInfo{})
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
}

func runCLIJSON(t *testing.T, args ...string) map[string]any {
	t.Helper()
	ensureDefaultAgentsDirResolverForTest(t)

	var stdout strings.Builder
	var stderr strings.Builder
	exitCode := run(context.Background(), args, &stdout, &stderr, buildInfo{})
	if exitCode != 0 {
		t.Fatalf("expected success for args %v, got exit code %d and stderr %s", args, exitCode, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout.String()), &payload); err != nil {
		t.Fatalf("stdout was not valid JSON for args %v: %v\noutput=%s", args, err, stdout.String())
	}

	return payload
}

func runCLIText(t *testing.T, args ...string) string {
	t.Helper()
	ensureDefaultAgentsDirResolverForTest(t)

	var stdout strings.Builder
	var stderr strings.Builder
	exitCode := run(context.Background(), args, &stdout, &stderr, buildInfo{})
	if exitCode != 0 {
		t.Fatalf("expected success for args %v, got exit code %d and stderr %s", args, exitCode, stderr.String())
	}

	return stdout.String()
}

func runCLITextInput(t *testing.T, input string, args ...string) string {
	t.Helper()
	ensureDefaultAgentsDirResolverForTest(t)

	var stdout strings.Builder
	var stderr strings.Builder
	exitCode := runWithIO(context.Background(), args, strings.NewReader(input), &stdout, &stderr, buildInfo{})
	if exitCode != 0 {
		t.Fatalf("expected success for args %v, got exit code %d and stderr %s", args, exitCode, stderr.String())
	}

	return stdout.String()
}
