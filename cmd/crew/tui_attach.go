package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	runtimeadapter "crew/internal/adapters/runtime"
	"crew/internal/application"
	"crew/internal/domain"
	"crew/internal/platform"
)

func (s *runtimeState) runTUISessionView(ctx context.Context, in io.Reader, out io.Writer, options liveViewOptions) error {
	if !supportsFullScreenTUI(in, out) {
		return s.runInteractiveSessionView(ctx, in, out, options)
	}

	if options.PollInterval <= 0 {
		if err := s.bootstrap(); err != nil {
			return err
		}
		options.PollInterval = time.Duration(s.loaded.Config.UI.RefreshIntervalMillis) * time.Millisecond
		if options.PollInterval <= 0 {
			options.PollInterval = 250 * time.Millisecond
		}
	}

	sendConversationID := options.ConversationID
	if sendConversationID == "" {
		sendConversationID = domain.ConversationID("conversation-1")
	}

	_, err := s.withLocalRuntime(ctx, true, func(rt *runtimeadapter.Runtime) (any, error) {
		actualAgentsDir, err := loadSessionAgentsDir(ctx, rt, options.AgentsDir, options.SessionID)
		if err != nil {
			return nil, err
		}
		options.AgentsDir = actualAgentsDir

		model := newAttachModel(ctx, rt, options, sendConversationID, s.loaded.Config.UI)
		program := tea.NewProgram(
			model,
			tea.WithAltScreen(),
			tea.WithMouseCellMotion(),
			tea.WithInput(in),
			tea.WithOutput(out),
		)
		_, err = program.Run()
		return nil, err
	})
	return err
}

func supportsFullScreenTUI(in io.Reader, out io.Writer) bool {
	inFile, inOK := in.(*os.File)
	outFile, outOK := out.(*os.File)
	if !inOK || !outOK {
		return false
	}
	return term.IsTerminal(int(inFile.Fd())) && term.IsTerminal(int(outFile.Fd()))
}

type attachRoomState struct {
	snapshot       runtimeadapter.SessionSnapshot
	tasks          []application.SandboxTask
	vectorState    application.VectorIndexState
	vectorBackend  application.VectorIndexStatus
	providers      runtimeadapter.ProviderCatalog
	conversations  []domain.ConversationID
	activeTaskByID map[application.AgentTaskID]application.SandboxTask
}

type attachModel struct {
	ctx                   context.Context
	rt                    *runtimeadapter.Runtime
	options               liveViewOptions
	ui                    platform.UIConfig
	agentsDir             string
	sendConversationID    domain.ConversationID
	selectedConvID        domain.ConversationID
	splitPanes            bool
	agents                []domain.Agent
	agentColors           map[string]string
	colorSeed             uint64
	styles                attachStyles
	input                 textinput.Model
	inputAssist           attachInputAssist
	viewport              viewport.Model
	stickyBottom          bool
	room                  attachRoomState
	optimistic            []optimisticMessage
	localNotices          []attachDisplayEvent
	inputHistory          []string
	historyIndex          int
	historyDraft          string
	nextOptimisticID      int
	status                string
	lastError             string
	pendingOps            int
	pendingAgentStates    map[domain.AgentID]string
	width                 int
	height                int
	layoutBodyHeight      int
	layoutMainWidth       int
	layoutRoomWidth       int
	layoutRoomHeight      int
	layoutRoomInnerWidth  int
	layoutRoomInnerHeight int
	layoutPreviewWidth    int
	layoutPreviewHeight   int
	layoutSidebarWidth    int
	layoutSidebarHeight   int
	lastViewportContent   string
	lastViewportWidth     int
	lastViewportHeight    int
	showSidebar           bool
}

type optimisticMessage struct {
	ID             string
	ConversationID domain.ConversationID
	Sender         string
	Body           string
	ToAgentIDs     []domain.AgentID
	SubmittedAt    time.Time
}

type attachInputAssistKind string

const (
	attachInputAssistNone    attachInputAssistKind = ""
	attachInputAssistCommand attachInputAssistKind = "command"
	attachInputAssistMention attachInputAssistKind = "mention"
)

type attachInputAssist struct {
	Kind        attachInputAssistKind
	Start       int
	End         int
	Suggestions []attachInputSuggestion
	Selected    int
}

type attachInputSuggestion struct {
	Label       string
	InsertValue string
	Description string
}

type attachSlashCommand struct {
	Command     string
	InsertValue string
	Description string
}

type attachDisplayEvent struct {
	Kind           string
	RecordedAt     time.Time
	ConversationID domain.ConversationID
	Sender         string
	Body           string
	ReplyTo        domain.MessageID
	ToAgentIDs     []domain.AgentID
	ReplySummary   string
	Pending        bool
}

type attachRoomStateMsg struct {
	state attachRoomState
}

type attachDispatchCompleteMsg struct {
	state     attachRoomState
	request   attachDispatchRequest
	autoSteps int
}

type attachStepProgressMsg struct {
	state     attachRoomState
	step      application.SessionStepResult
	remaining int
}

type attachErrMsg struct {
	err error
}

type attachTickMsg time.Time
type attachBeginDispatchMsg struct {
	request   attachDispatchRequest
	autoSteps int
}
type attachContinueAutoMsg struct {
	remaining int
}

type attachDispatchRequest struct {
	ID         string
	Body       string
	ToAgentIDs []domain.AgentID
}

type attachStyles struct {
	frame         lipgloss.Style
	header        lipgloss.Style
	subheader     lipgloss.Style
	status        lipgloss.Style
	errorText     lipgloss.Style
	inputLabel    lipgloss.Style
	inputBox      lipgloss.Style
	sidebar       lipgloss.Style
	room          lipgloss.Style
	preview       lipgloss.Style
	footer        lipgloss.Style
	muted         lipgloss.Style
	system        lipgloss.Style
	task          lipgloss.Style
	pendingSender lipgloss.Style
	messageBody   lipgloss.Style
	blockHeader   lipgloss.Style
	sectionTitle  lipgloss.Style
	statusGood    lipgloss.Style
	statusWarn    lipgloss.Style
	statusBusy    lipgloss.Style
	inputAssist   lipgloss.Style
}

var attachSlashCommands = []attachSlashCommand{
	{Command: "/help", InsertValue: "/help", Description: "show attach commands"},
	{Command: "/step", InsertValue: "/step", Description: "run one free-mode turn"},
	{Command: "/auto", InsertValue: "/auto ", Description: "run bounded free-mode turns"},
	{Command: "/quit", InsertValue: "/quit", Description: "exit the attached room"},
}

func newAttachModel(
	ctx context.Context,
	rt *runtimeadapter.Runtime,
	options liveViewOptions,
	sendConversationID domain.ConversationID,
	ui platform.UIConfig,
) attachModel {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "Type a message or /help"
	input.CharLimit = 4096
	input.Focus()

	model := attachModel{
		ctx:                ctx,
		rt:                 rt,
		options:            options,
		ui:                 ui,
		agentsDir:          options.AgentsDir,
		sendConversationID: sendConversationID,
		selectedConvID:     sendConversationID,
		splitPanes:         ui.AttachSplitPanes,
		agents:             nil,
		agentColors:        make(map[string]string),
		colorSeed:          uint64(time.Now().UnixNano()),
		styles:             newAttachStyles(ui),
		input:              input,
		viewport:           viewport.New(0, 0),
		stickyBottom:       true,
		historyIndex:       -1,
		status:             fmt.Sprintf("attached to %s / %s", options.SessionID, sendConversationID),
		pendingAgentStates: make(map[domain.AgentID]string),
	}
	model.agents, model.agentColors = mustLoadAttachAgents(model.agentsDir)
	model.refreshInputAssist()
	model.viewport.Style = model.styles.room
	model.viewport.SetContent(model.styles.muted.Render("Loading room..."))
	return model
}

func mustLoadAttachAgents(agentsDir string) ([]domain.Agent, map[string]string) {
	var err error
	if strings.TrimSpace(agentsDir) == "" {
		agentsDir, err = resolveSelectedAgentsDir("")
		if err != nil {
			return nil, map[string]string{}
		}
	}
	entries, err := runtimeadapter.LoadAgentCatalogDir(agentsDir)
	if err != nil {
		return nil, map[string]string{}
	}
	agents := make([]domain.Agent, 0, len(entries))
	colors := make(map[string]string, len(entries))
	for _, entry := range entries {
		agents = append(agents, entry.Agent)
		if entry.Color != "" {
			colors[string(entry.Agent.ID)] = entry.Color
		}
	}
	return agents, colors
}

func newAttachStyles(ui platform.UIConfig) attachStyles {
	bg := "#1c1917"
	panel := "#292524"
	border := "#57534e"
	text := "#f5f5f4"
	muted := "#d6d3d1"
	accent := "#f97316"
	good := "#34d399"
	warn := "#fbbf24"
	busy := "#38bdf8"
	if ui.Theme == "graphite" {
		bg = "#111827"
		panel = "#1f2937"
		border = "#4b5563"
		text = "#f9fafb"
		muted = "#9ca3af"
		accent = "#38bdf8"
		good = "#22c55e"
		warn = "#f59e0b"
		busy = "#60a5fa"
	}

	return attachStyles{
		frame:         lipgloss.NewStyle().Background(lipgloss.Color(bg)).Foreground(lipgloss.Color(text)),
		header:        lipgloss.NewStyle().Foreground(lipgloss.Color(text)).Background(lipgloss.Color(panel)).Bold(true).Padding(0, 1),
		subheader:     lipgloss.NewStyle().Foreground(lipgloss.Color(muted)).Background(lipgloss.Color(panel)).Padding(0, 1),
		status:        lipgloss.NewStyle().Foreground(lipgloss.Color(accent)).Bold(true),
		errorText:     lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444")).Bold(true),
		inputLabel:    lipgloss.NewStyle().Foreground(lipgloss.Color(accent)).Bold(true),
		inputBox:      lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color(border)).Padding(0, 1),
		sidebar:       lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(border)).Padding(0, 1),
		room:          lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(border)).Padding(0, 1),
		preview:       lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(border)).Padding(0, 1),
		footer:        lipgloss.NewStyle().Foreground(lipgloss.Color(muted)),
		muted:         lipgloss.NewStyle().Foreground(lipgloss.Color(muted)),
		system:        lipgloss.NewStyle().Foreground(lipgloss.Color("#fbbf24")),
		task:          lipgloss.NewStyle().Foreground(lipgloss.Color("#a78bfa")),
		pendingSender: lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8")).Italic(true),
		messageBody:   lipgloss.NewStyle().PaddingLeft(2),
		blockHeader:   lipgloss.NewStyle().Bold(true),
		sectionTitle:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(accent)),
		statusGood:    lipgloss.NewStyle().Foreground(lipgloss.Color(good)).Bold(true),
		statusWarn:    lipgloss.NewStyle().Foreground(lipgloss.Color(warn)).Bold(true),
		statusBusy:    lipgloss.NewStyle().Foreground(lipgloss.Color(busy)).Bold(true),
		inputAssist:   lipgloss.NewStyle().Foreground(lipgloss.Color(muted)),
	}
}

func (m attachModel) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		attachFetchRoomStateCmd(m.ctx, m.rt, m.options.SessionID),
		attachTickCmd(m.options.PollInterval),
	)
}

func (m attachModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = typed.Width
		m.height = typed.Height
		m.layout()
		m.syncViewportContent(false)
		return m, nil
	case attachRoomStateMsg:
		m.room = typed.state
		m.ensureActiveConversation()
		m.lastError = ""
		m.syncViewportContent(false)
		return m, nil
	case attachDispatchCompleteMsg:
		m.room = typed.state
		m.ensureActiveConversation()
		m.popOptimistic(typed.request.ID)
		m.lastError = ""
		m.status = "operator message sent"
		if typed.autoSteps > 0 {
			m.pendingOps = 1
			m.setPendingSequence(typed.autoSteps)
			m.status = fmt.Sprintf("operator message sent, auto running %d turn(s)", typed.autoSteps)
			m.syncViewportContent(true)
			return m, attachContinueAutoTickCmd(typed.autoSteps)
		}
		m.syncViewportContent(true)
		return m, nil
	case attachStepProgressMsg:
		m.room = typed.state
		m.ensureActiveConversation()
		m.lastError = ""
		if typed.remaining > 1 && typed.step.Stepped {
			m.pendingOps = 1
			m.setPendingSequence(typed.remaining - 1)
			if typed.step.Agent != nil {
				m.status = fmt.Sprintf("%s replied, continuing auto run (%d left)", typed.step.Agent.ID, typed.remaining-1)
			} else {
				m.status = fmt.Sprintf("continuing auto run (%d left)", typed.remaining-1)
			}
			m.syncViewportContent(true)
			return m, attachContinueAutoTickCmd(typed.remaining - 1)
		}
		m.pendingOps = 0
		clear(m.pendingAgentStates)
		m.status = fmt.Sprintf("step=%t reason=%s", typed.step.Stepped, typed.step.Reason)
		if typed.step.Agent != nil {
			m.status = fmt.Sprintf("step agent=%s", typed.step.Agent.ID)
		}
		if !typed.step.Stepped && typed.step.Reason != "" {
			m.status = fmt.Sprintf("stopped: %s", typed.step.Reason)
		}
		m.syncViewportContent(true)
		return m, nil
	case attachErrMsg:
		m.pendingOps = 0
		clear(m.pendingAgentStates)
		m.lastError = typed.err.Error()
		m.appendLocalNotice(attachDisplayEvent{
			Kind:           "system",
			RecordedAt:     time.Now().UTC(),
			ConversationID: m.sendConversationID,
			Body:           "room error: " + typed.err.Error(),
		})
		m.syncViewportContent(true)
		return m, nil
	case attachTickMsg:
		return m, tea.Batch(
			attachFetchRoomStateCmd(m.ctx, m.rt, m.options.SessionID),
			attachTickCmd(m.options.PollInterval),
		)
	case attachBeginDispatchMsg:
		return m, attachDispatchCmd(m.ctx, m.rt, m.options.SessionID, m.sendConversationID, typed.request, typed.autoSteps)
	case attachContinueAutoMsg:
		return m, attachRunStepCmd(m.ctx, m.rt, m.options.SessionID, m.sendConversationID, m.options.Orchestration, m.options.ReplyRouting, typed.remaining)
	case tea.KeyMsg:
		switch typed.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "enter":
			if m.acceptSelectedInputAssist(false) {
				return m, nil
			}
			value := strings.TrimSpace(m.input.Value())
			if value == "" {
				return m, nil
			}
			m.recordHistory(value)
			m.input.SetValue("")
			m.refreshInputAssist()
			return m, m.submitInput(value)
		case "ctrl+l":
			return m, attachFetchRoomStateCmd(m.ctx, m.rt, m.options.SessionID)
		case "up":
			if m.selectPreviousInputAssist() {
				return m, nil
			}
			m.historyUp()
			return m, nil
		case "down":
			if m.selectNextInputAssist() {
				return m, nil
			}
			m.historyDown()
			return m, nil
		case "pgup":
			m.viewport.HalfViewUp()
			m.stickyBottom = m.viewport.AtBottom()
			return m, nil
		case "pgdown":
			m.viewport.HalfViewDown()
			m.stickyBottom = m.viewport.AtBottom()
			return m, nil
		case "home":
			m.viewport.GotoTop()
			m.stickyBottom = false
			return m, nil
		case "end":
			m.viewport.GotoBottom()
			m.stickyBottom = true
			return m, nil
		case "ctrl+u":
			m.input.SetValue("")
			m.refreshInputAssist()
			return m, nil
		case "ctrl+d":
			m.showRecentInputStatus()
			return m, nil
		case "tab":
			if m.acceptSelectedInputAssist(true) {
				return m, nil
			}
			if m.canSplitConversations() {
				m.splitPanes = !m.splitPanes
				m.layout()
				m.syncViewportContent(true)
				if m.splitPanes {
					m.status = "split panes enabled"
				} else {
					m.status = "split panes disabled"
				}
			}
			return m, nil
		case "shift+tab":
			if m.selectPreviousInputAssist() {
				return m, nil
			}
			return m, nil
		case "]":
			if m.options.ConversationID == "" {
				m.cycleConversation(1)
			}
			return m, nil
		case "[":
			if m.options.ConversationID == "" {
				m.cycleConversation(-1)
			}
			return m, nil
		}
	}

	var (
		cmd   tea.Cmd
		vpCmd tea.Cmd
	)
	m.viewport, vpCmd = m.viewport.Update(msg)
	m.stickyBottom = m.viewport.AtBottom()
	m.input, cmd = m.input.Update(msg)
	m.refreshInputAssist()
	return m, tea.Batch(vpCmd, cmd)
}

func (m attachModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading crew TUI..."
	}

	header := m.renderHeader()
	body := m.renderBody()
	input := m.renderInput()
	footer := m.renderFooter()

	return m.styles.frame.
		Width(m.width).
		Height(m.height).
		Render(lipgloss.JoinVertical(lipgloss.Left, header, body, input, footer))
}

func (m *attachModel) submitInput(value string) tea.Cmd {
	if strings.HasPrefix(value, "/") {
		return m.handleCommand(value)
	}

	request := m.newAttachDispatchRequest(value)
	channel, _ := attachDispatchRouting(request.ToAgentIDs)
	effectiveAutoSteps := effectiveAttachAutoSteps(m.options.AutoSteps, request.ToAgentIDs)
	m.selectedConvID = m.sendConversationID
	m.stickyBottom = true
	if effectiveAutoSteps > 0 {
		history := append(
			append([]domain.Message(nil), activeConversationMessages(m.room.snapshot.Messages, m.sendConversationID)...),
			domain.Message{
				SessionID:      m.options.SessionID,
				ConversationID: m.sendConversationID,
				Sender:         domain.UserSender("operator"),
				ToAgentIDs:     append([]domain.AgentID(nil), request.ToAgentIDs...),
				Channel:        channel,
				Kind:           domain.MessageKindUtterance,
				Body:           request.Body,
				Timestamp:      time.Now().UTC(),
			},
		)
		m.pendingOps = 1
		m.setPendingSequenceFromHistory(history, effectiveAutoSteps)
	}
	m.optimistic = append(m.optimistic, optimisticMessage{
		ID:             request.ID,
		ConversationID: m.sendConversationID,
		Sender:         "operator",
		Body:           request.Body,
		ToAgentIDs:     append([]domain.AgentID(nil), request.ToAgentIDs...),
		SubmittedAt:    time.Now().UTC(),
	})
	m.status = "sending operator message..."
	if len(request.ToAgentIDs) > 0 {
		targets := make([]string, 0, len(request.ToAgentIDs))
		for _, id := range request.ToAgentIDs {
			targets = append(targets, string(id))
		}
		m.status = "sending operator message to " + strings.Join(targets, ", ")
	}
	if effectiveAutoSteps > 0 {
		m.status = fmt.Sprintf("%s, auto queued for %d turn(s)", m.status, effectiveAutoSteps)
	}
	m.syncViewportContent(true)
	return attachBeginDispatchTickCmd(request, effectiveAutoSteps)
}

func (m *attachModel) newAttachDispatchRequest(value string) attachDispatchRequest {
	requestID := fmt.Sprintf("optimistic-%d", m.nextOptimisticID)
	m.nextOptimisticID++
	return attachDispatchRequest{
		ID:         requestID,
		Body:       value,
		ToAgentIDs: mentionedAgentIDs(value, m.agents),
	}
}

func attachDispatchRouting(recipients []domain.AgentID) (domain.MessageChannel, *domain.ConversationPolicy) {
	if len(recipients) == 0 {
		return domain.MessageChannelUser, nil
	}
	policy := domain.DefaultConversationPolicy()
	policy.RequireReplyTargetForDirect = false
	return domain.MessageChannelDirect, &policy
}

func effectiveAttachAutoSteps(configured int, recipients []domain.AgentID) int {
	return max(configured, len(recipients))
}

func mentionedAgentIDs(body string, agents []domain.Agent) []domain.AgentID {
	matches := mentionedAgentSet(body, agents)
	if len(matches) == 0 {
		return nil
	}
	runes := []rune(body)
	ids := make([]domain.AgentID, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for idx := 0; idx < len(runes); idx++ {
		if runes[idx] != '@' {
			continue
		}
		start := idx + 1
		end := start
		for end < len(runes) && !isInputAssistWhitespace(runes[end]) {
			end++
		}
		if start == end {
			continue
		}
		token := strings.ToLower(string(runes[start:end]))
		agentID, exists := matches[token]
		if !exists {
			continue
		}
		if _, exists := seen[string(agentID)]; exists {
			continue
		}
		seen[string(agentID)] = struct{}{}
		ids = append(ids, agentID)
	}
	return ids
}

func mentionedAgentSet(body string, agents []domain.Agent) map[string]domain.AgentID {
	lookup := make(map[string]domain.AgentID, len(agents))
	for _, agent := range agents {
		lookup[strings.ToLower(string(agent.ID))] = agent.ID
	}
	matches := make(map[string]domain.AgentID)
	for _, field := range strings.Fields(body) {
		token := sanitizeMentionLookupToken(field)
		if token == "" {
			continue
		}
		id, exists := lookup[strings.ToLower(token)]
		if !exists {
			continue
		}
		matches[strings.ToLower(token)] = id
	}
	return matches
}

func sanitizeMentionLookupToken(field string) string {
	if !strings.HasPrefix(field, "@") || len(field) < 2 {
		return ""
	}
	token := strings.TrimLeft(field, "@")
	token = strings.TrimFunc(token, func(r rune) bool {
		return !isMentionTokenRune(r)
	})
	if token == "" {
		return ""
	}
	for _, r := range token {
		if !isMentionTokenRune(r) {
			return ""
		}
	}
	return token
}

func isMentionTokenRune(r rune) bool {
	return r == '_' || r == '-' ||
		(r >= '0' && r <= '9') ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z')
}

func (m *attachModel) handleCommand(raw string) tea.Cmd {
	fields := strings.Fields(raw)
	switch fields[0] {
	case "/quit", "/exit":
		return tea.Quit
	case "/help":
		m.status = interactiveHelpText(m.options.AutoSteps)
		return nil
	case "/step":
		m.selectedConvID = m.sendConversationID
		m.stickyBottom = true
		m.pendingOps = 1
		m.setPendingSequence(1)
		m.status = "running one agent turn..."
		m.syncViewportContent(true)
		return attachRunStepCmd(m.ctx, m.rt, m.options.SessionID, m.sendConversationID, m.options.Orchestration, m.options.ReplyRouting, 1)
	case "/auto":
		maxSteps := 3
		if len(fields) > 1 {
			value, err := strconv.Atoi(fields[1])
			if err != nil || value < 1 {
				return func() tea.Msg {
					return attachErrMsg{err: newCLIError("invalid_arguments", "usage: /auto [positive-step-count]")}
				}
			}
			maxSteps = value
		}
		m.selectedConvID = m.sendConversationID
		m.stickyBottom = true
		m.pendingOps = 1
		m.setPendingSequence(maxSteps)
		m.status = fmt.Sprintf("running auto for %d turn(s)...", maxSteps)
		m.syncViewportContent(true)
		return attachRunStepCmd(m.ctx, m.rt, m.options.SessionID, m.sendConversationID, m.options.Orchestration, m.options.ReplyRouting, maxSteps)
	default:
		return func() tea.Msg {
			return attachErrMsg{err: newCLIError("invalid_arguments", fmt.Sprintf("unknown interactive command %q", fields[0]))}
		}
	}
}

func (m *attachModel) ensureActiveConversation() {
	if m.options.ConversationID != "" {
		m.selectedConvID = m.options.ConversationID
		m.sendConversationID = m.options.ConversationID
		return
	}
	if containsConversationID(m.room.conversations, m.selectedConvID) {
		return
	}
	if containsConversationID(m.room.conversations, m.sendConversationID) {
		m.selectedConvID = m.sendConversationID
		return
	}
	if len(m.room.conversations) > 0 {
		m.selectedConvID = m.room.conversations[0]
		m.sendConversationID = m.selectedConvID
	}
}

func containsConversationID(ids []domain.ConversationID, target domain.ConversationID) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func (m *attachModel) cycleConversation(delta int) {
	if len(m.room.conversations) == 0 {
		return
	}
	idx := 0
	for i, id := range m.room.conversations {
		if id == m.selectedConvID {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(m.room.conversations)) % len(m.room.conversations)
	m.selectedConvID = m.room.conversations[idx]
	m.sendConversationID = m.selectedConvID
	m.stickyBottom = true
	m.status = fmt.Sprintf("active conversation: %s", m.selectedConvID)
	m.layout()
	m.syncViewportContent(true)
}

func (m *attachModel) setPendingSequence(maxSteps int) {
	m.setPendingSequenceFromHistory(
		activeConversationMessages(m.room.snapshot.Messages, m.sendConversationID),
		maxSteps,
	)
}

func (m *attachModel) setPendingSequenceFromHistory(history []domain.Message, maxSteps int) {
	clear(m.pendingAgentStates)
	sequence := estimatePendingSequence(
		history,
		m.agents,
		m.options.Orchestration,
		maxSteps,
	)
	for idx, agentID := range sequence {
		if idx == 0 {
			m.pendingAgentStates[agentID] = "thinking"
			continue
		}
		m.pendingAgentStates[agentID] = "queued"
	}
}

func activeConversationMessages(messages []domain.Message, conversationID domain.ConversationID) []domain.Message {
	filtered := make([]domain.Message, 0, len(messages))
	for _, message := range messages {
		if conversationID != "" && message.ConversationID != conversationID {
			continue
		}
		filtered = append(filtered, message)
	}
	return filtered
}

func estimatePendingSequence(messages []domain.Message, agents []domain.Agent, mode application.OrchestrationMode, maxSteps int) []domain.AgentID {
	history := append([]domain.Message(nil), messages...)
	sequence := make([]domain.AgentID, 0, maxSteps)
	for step := 0; step < maxSteps; step++ {
		if len(history) == 0 {
			break
		}
		lastMessage := history[len(history)-1]
		candidates := eligibleAgentsForUI(history, lastMessage, agents)
		if len(candidates) == 0 {
			break
		}
		ordered := orderCandidatesForUI(mode, agents, candidates, &lastMessage)
		if len(ordered) == 0 {
			break
		}
		selected := ordered[0]
		sequence = append(sequence, selected.ID)
		history = append(history, domain.Message{
			SessionID:      lastMessage.SessionID,
			ConversationID: lastMessage.ConversationID,
			Sender:         domain.AgentSender(selected.ID),
			Channel:        domain.MessageChannelBroadcast,
			Kind:           domain.MessageKindUtterance,
			Body:           "",
			ReplyTo:        lastMessage.ID,
			Timestamp:      time.Now().UTC(),
		})
	}
	return sequence
}

func eligibleAgentsForUI(history []domain.Message, lastMessage domain.Message, agents []domain.Agent) []domain.Agent {
	candidates := make([]domain.Agent, 0, len(agents))
	for _, agent := range agents {
		if agentIneligibleForUI(agent, history, lastMessage) {
			continue
		}
		candidates = append(candidates, agent)
	}
	return candidates
}

func agentIneligibleForUI(agent domain.Agent, history []domain.Message, lastMessage domain.Message) bool {
	if lastMessage.Sender.Type == domain.MessageSenderTypeAgent && lastMessage.Sender.ID == string(agent.ID) {
		limit := agent.Policies.MaxConsecutiveTurns
		if policyLimit := domain.DefaultConversationPolicy().MaxConsecutiveTurnsPerAgent; policyLimit < limit {
			limit = policyLimit
		}
		if consecutiveAgentTurnsForUI(history, agent.ID) >= limit {
			return true
		}
	}
	if agent.Policies.RequireDirectMention && !messageTargetsAgentForUI(lastMessage, agent.ID) {
		return true
	}
	if lastMessage.Channel == domain.MessageChannelBroadcast && !agent.Policies.AllowBroadcast {
		return true
	}
	if lastMessage.Channel == domain.MessageChannelDirect && len(lastMessage.ToAgentIDs) > 0 && !containsAgentIDForUI(lastMessage.ToAgentIDs, agent.ID) {
		return true
	}
	return false
}

func consecutiveAgentTurnsForUI(history []domain.Message, agentID domain.AgentID) int {
	count := 0
	for idx := len(history) - 1; idx >= 0; idx-- {
		message := history[idx]
		if message.Sender.Type != domain.MessageSenderTypeAgent || message.Sender.ID != string(agentID) {
			break
		}
		count++
	}
	return count
}

func containsAgentIDForUI(ids []domain.AgentID, target domain.AgentID) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func messageTargetsAgentForUI(message domain.Message, agentID domain.AgentID) bool {
	if containsAgentIDForUI(message.ToAgentIDs, agentID) {
		return true
	}
	body := strings.ToLower(message.Body)
	return strings.Contains(body, strings.ToLower(string(agentID)))
}

func orderCandidatesForUI(
	mode application.OrchestrationMode,
	allAgents []domain.Agent,
	candidates []domain.Agent,
	lastMessage *domain.Message,
) []domain.Agent {
	all := append([]domain.Agent(nil), allAgents...)
	if len(all) == 0 {
		all = append([]domain.Agent(nil), candidates...)
	}
	sortAgentsForUI(all)
	orderedAll := reorderAgentsForModeForUI(mode, all, lastMessage)
	candidateMap := make(map[domain.AgentID]domain.Agent, len(candidates))
	for _, candidate := range candidates {
		candidateMap[candidate.ID] = candidate
	}
	ordered := make([]domain.Agent, 0, len(candidates))
	for _, agent := range orderedAll {
		candidate, exists := candidateMap[agent.ID]
		if !exists {
			continue
		}
		ordered = append(ordered, candidate)
	}
	return ordered
}

func sortAgentsForUI(agents []domain.Agent) {
	for i := 0; i < len(agents); i++ {
		for j := i + 1; j < len(agents); j++ {
			if compareAgentsForUI(agents[j], agents[i]) < 0 {
				agents[i], agents[j] = agents[j], agents[i]
			}
		}
	}
}

func compareAgentsForUI(a, b domain.Agent) int {
	if a.Policies.Priority > b.Policies.Priority {
		return -1
	}
	if a.Policies.Priority < b.Policies.Priority {
		return 1
	}
	if a.Policies.Weight > b.Policies.Weight {
		return -1
	}
	if a.Policies.Weight < b.Policies.Weight {
		return 1
	}
	if a.ID < b.ID {
		return -1
	}
	if a.ID > b.ID {
		return 1
	}
	return 0
}

func reorderAgentsForModeForUI(mode application.OrchestrationMode, agents []domain.Agent, lastMessage *domain.Message) []domain.Agent {
	ordered := append([]domain.Agent(nil), agents...)
	if lastMessage == nil {
		return ordered
	}
	switch normalizeUIMode(mode) {
	case application.OrchestrationModeRoundRobin:
		if lastMessage.Sender.Type != domain.MessageSenderTypeAgent {
			return ordered
		}
		start := 0
		for idx, agent := range ordered {
			if agent.ID == domain.AgentID(lastMessage.Sender.ID) {
				start = (idx + 1) % len(ordered)
				break
			}
		}
		rotated := make([]domain.Agent, 0, len(ordered))
		rotated = append(rotated, ordered[start:]...)
		rotated = append(rotated, ordered[:start]...)
		return rotated
	case application.OrchestrationModeMentionedFirst:
		mentioned := make([]domain.Agent, 0, len(ordered))
		others := make([]domain.Agent, 0, len(ordered))
		for _, agent := range ordered {
			if messageTargetsAgentForUI(*lastMessage, agent.ID) {
				mentioned = append(mentioned, agent)
				continue
			}
			others = append(others, agent)
		}
		if len(mentioned) == 0 {
			return reorderAgentsForModeForUI(application.OrchestrationModeDeterministic, ordered, lastMessage)
		}
		return append(mentioned, others...)
	default:
		if lastMessage.Sender.Type != domain.MessageSenderTypeAgent {
			return ordered
		}
		lastSender := domain.AgentID(lastMessage.Sender.ID)
		head := make([]domain.Agent, 0, len(ordered))
		tail := make([]domain.Agent, 0, 1)
		for _, agent := range ordered {
			if agent.ID == lastSender {
				tail = append(tail, agent)
				continue
			}
			head = append(head, agent)
		}
		return append(head, tail...)
	}
}

func normalizeUIMode(mode application.OrchestrationMode) application.OrchestrationMode {
	if mode == "" {
		return application.OrchestrationModeDeterministic
	}
	return mode
}

func (m *attachModel) recordHistory(value string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return
	}
	if len(m.inputHistory) == 0 || m.inputHistory[len(m.inputHistory)-1] != trimmed {
		m.inputHistory = append(m.inputHistory, trimmed)
	}
	if len(m.inputHistory) > 50 {
		m.inputHistory = m.inputHistory[len(m.inputHistory)-50:]
	}
	m.historyIndex = -1
	m.historyDraft = ""
}

func (m *attachModel) historyUp() {
	if len(m.inputHistory) == 0 {
		return
	}
	if m.historyIndex == -1 {
		m.historyDraft = m.input.Value()
		m.historyIndex = len(m.inputHistory) - 1
	} else if m.historyIndex > 0 {
		m.historyIndex--
	}
	m.input.SetValue(m.inputHistory[m.historyIndex])
	m.input.CursorEnd()
	m.refreshInputAssist()
}

func (m *attachModel) historyDown() {
	if len(m.inputHistory) == 0 || m.historyIndex == -1 {
		return
	}
	if m.historyIndex < len(m.inputHistory)-1 {
		m.historyIndex++
		m.input.SetValue(m.inputHistory[m.historyIndex])
	} else {
		m.historyIndex = -1
		m.input.SetValue(m.historyDraft)
	}
	m.input.CursorEnd()
	m.refreshInputAssist()
}

func (m *attachModel) refreshInputAssist() {
	value := m.input.Value()
	cursor := m.input.Position()
	if cursor < 0 {
		cursor = 0
	}
	assist := m.deriveInputAssist(value, cursor)
	if assist.Kind == attachInputAssistNone || len(assist.Suggestions) == 0 {
		m.inputAssist = attachInputAssist{}
		return
	}
	if m.inputAssist.Kind == assist.Kind &&
		m.inputAssist.Start == assist.Start &&
		m.inputAssist.End == assist.End &&
		len(m.inputAssist.Suggestions) > 0 {
		selectedLabel := m.inputAssist.Suggestions[min(m.inputAssist.Selected, len(m.inputAssist.Suggestions)-1)].Label
		for idx, suggestion := range assist.Suggestions {
			if suggestion.Label == selectedLabel {
				assist.Selected = idx
				break
			}
		}
	}
	m.inputAssist = assist
}

func (m attachModel) deriveInputAssist(value string, cursor int) attachInputAssist {
	if assist := m.deriveCommandInputAssist(value, cursor); assist.Kind != attachInputAssistNone {
		return assist
	}
	return m.deriveMentionInputAssist(value, cursor)
}

func (m attachModel) deriveCommandInputAssist(value string, cursor int) attachInputAssist {
	runes := []rune(value)
	cursor = min(max(cursor, 0), len(runes))
	if len(runes) == 0 || runes[0] != '/' {
		return attachInputAssist{}
	}
	commandEnd := len(runes)
	for idx, r := range runes {
		if isInputAssistWhitespace(r) {
			commandEnd = idx
			break
		}
	}
	if cursor > commandEnd {
		return attachInputAssist{}
	}
	query := strings.ToLower(string(runes[1:cursor]))
	suggestions := make([]attachInputSuggestion, 0, len(attachSlashCommands))
	for _, command := range attachSlashCommands {
		if query != "" && !strings.HasPrefix(strings.ToLower(strings.TrimPrefix(command.Command, "/")), query) {
			continue
		}
		suggestions = append(suggestions, attachInputSuggestion{
			Label:       command.Command,
			InsertValue: command.InsertValue,
			Description: command.Description,
		})
	}
	if len(suggestions) == 0 {
		return attachInputAssist{}
	}
	return attachInputAssist{
		Kind:        attachInputAssistCommand,
		Start:       0,
		End:         commandEnd,
		Suggestions: suggestions,
	}
}

func (m attachModel) deriveMentionInputAssist(value string, cursor int) attachInputAssist {
	runes := []rune(value)
	cursor = min(max(cursor, 0), len(runes))
	if len(runes) == 0 {
		return attachInputAssist{}
	}
	start := cursor
	for start > 0 && !isInputAssistWhitespace(runes[start-1]) {
		start--
	}
	end := cursor
	for end < len(runes) && !isInputAssistWhitespace(runes[end]) {
		end++
	}
	if start >= len(runes) || runes[start] != '@' || cursor <= start {
		return attachInputAssist{}
	}
	query := strings.ToLower(string(runes[start+1 : cursor]))
	seen := mentionedAgentSet(value, m.agents)
	currentToken := strings.ToLower(strings.TrimPrefix(string(runes[start:end]), "@"))
	delete(seen, currentToken)
	suggestions := make([]attachInputSuggestion, 0, len(m.agents))
	for _, agent := range m.agents {
		idLower := strings.ToLower(string(agent.ID))
		nameLower := strings.ToLower(agent.Name)
		if query != "" && !strings.HasPrefix(idLower, query) && !strings.Contains(nameLower, query) {
			continue
		}
		if _, exists := seen[idLower]; exists {
			continue
		}
		label := "@" + string(agent.ID)
		if agent.Name != "" && agent.Name != string(agent.ID) {
			label += " " + agent.Name
		}
		suggestions = append(suggestions, attachInputSuggestion{
			Label:       label,
			InsertValue: "@" + string(agent.ID),
			Description: agent.Name,
		})
	}
	if len(suggestions) == 0 {
		return attachInputAssist{}
	}
	return attachInputAssist{
		Kind:        attachInputAssistMention,
		Start:       start,
		End:         end,
		Suggestions: suggestions,
	}
}

func (m *attachModel) selectNextInputAssist() bool {
	if len(m.inputAssist.Suggestions) == 0 {
		return false
	}
	m.inputAssist.Selected = (m.inputAssist.Selected + 1) % len(m.inputAssist.Suggestions)
	return true
}

func (m *attachModel) selectPreviousInputAssist() bool {
	if len(m.inputAssist.Suggestions) == 0 {
		return false
	}
	m.inputAssist.Selected--
	if m.inputAssist.Selected < 0 {
		m.inputAssist.Selected = len(m.inputAssist.Suggestions) - 1
	}
	return true
}

func (m *attachModel) acceptSelectedInputAssist(force bool) bool {
	if len(m.inputAssist.Suggestions) == 0 {
		return false
	}
	suggestion := m.inputAssist.Suggestions[m.inputAssist.Selected]
	if !force && m.currentInputAssistValue() == suggestion.InsertValue {
		return false
	}

	runes := []rune(m.input.Value())
	start := min(max(m.inputAssist.Start, 0), len(runes))
	end := min(max(m.inputAssist.End, start), len(runes))
	replacement := suggestion.InsertValue
	if m.inputAssist.Kind == attachInputAssistMention && (end == len(runes) || !isInputAssistWhitespace(runes[end])) {
		replacement += " "
	}
	updated := string(runes[:start]) + replacement + string(runes[end:])
	m.input.SetValue(updated)
	m.input.SetCursor(start + len([]rune(replacement)))
	m.refreshInputAssist()
	return true
}

func (m attachModel) currentInputAssistValue() string {
	if len(m.inputAssist.Suggestions) == 0 {
		return ""
	}
	runes := []rune(m.input.Value())
	start := min(max(m.inputAssist.Start, 0), len(runes))
	end := min(max(m.inputAssist.End, start), len(runes))
	value := string(runes[start:end])
	if m.inputAssist.Kind == attachInputAssistMention {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(value)
}

func isInputAssistWhitespace(r rune) bool {
	return r == ' ' || r == '\t'
}

func (m *attachModel) popOptimistic(id string) {
	for idx, entry := range m.optimistic {
		if entry.ID != id {
			continue
		}
		m.optimistic = append(m.optimistic[:idx], m.optimistic[idx+1:]...)
		return
	}
	if len(m.optimistic) > 0 {
		m.optimistic = m.optimistic[1:]
	}
}

func (m *attachModel) showRecentInputStatus() {
	if len(m.inputHistory) == 0 {
		m.status = "no input history yet"
		return
	}
	start := len(m.inputHistory) - 3
	if start < 0 {
		start = 0
	}
	m.status = "recent inputs: " + strings.Join(m.inputHistory[start:], " | ")
}

func (m *attachModel) layout() {
	totalWidth := max(m.width, 1)
	m.showSidebar = m.ui.AttachSidebar && totalWidth > 120
	m.layoutSidebarWidth = 0
	m.layoutSidebarHeight = 0
	m.layoutMainWidth = totalWidth
	if m.showSidebar {
		m.layoutSidebarWidth = 34
		m.layoutMainWidth = max(totalWidth-m.layoutSidebarWidth-1, 1)
	}

	m.input.Width = max(m.layoutMainWidth-m.styles.inputBox.GetHorizontalFrameSize(), 1)
	headerHeight := lipgloss.Height(m.renderHeader())
	inputHeight := lipgloss.Height(m.renderInput())
	footerHeight := lipgloss.Height(m.renderFooter())
	m.layoutBodyHeight = max(m.height-headerHeight-inputHeight-footerHeight, 1)
	m.layoutRoomHeight = m.layoutBodyHeight
	m.layoutPreviewHeight = m.layoutBodyHeight
	m.layoutSidebarHeight = m.layoutBodyHeight

	m.layoutRoomWidth = m.layoutMainWidth
	m.layoutPreviewWidth = 0
	if m.canSplitConversations() && m.splitPanes {
		roomWidth := (m.layoutMainWidth * 2) / 3
		previewWidth := m.layoutMainWidth - roomWidth - 1
		if roomWidth >= 40 && previewWidth >= 20 {
			m.layoutRoomWidth = roomWidth
			m.layoutPreviewWidth = previewWidth
		}
	}

	m.layoutRoomInnerWidth = max(m.layoutRoomWidth-m.styles.room.GetHorizontalFrameSize(), 1)
	m.layoutRoomInnerHeight = max(m.layoutRoomHeight-m.styles.room.GetVerticalFrameSize(), 1)
	m.viewport.Style = lipgloss.NewStyle()
	m.viewport.Width = m.layoutRoomInnerWidth
	m.viewport.Height = m.layoutRoomInnerHeight
}

func (m attachModel) renderHeader() string {
	width := max(m.width, 1)
	title := renderFixedStyledLine(
		m.styles.header,
		fmt.Sprintf(
			" crew room  session=%s  scope=%s  send=%s  mode=%s  status=%s ",
			m.options.SessionID,
			m.roomConversationLabel(),
			m.sendConversationID,
			m.room.snapshot.Session.Mode,
			m.room.snapshot.Session.Status,
		),
		width,
	)
	meta := renderFixedStyledLine(
		m.styles.subheader,
		fmt.Sprintf(
			" orchestration=%s  auto_steps=%d  poll=%s  theme=%s  pending_ops=%d ",
			displayOrchestrationMode(m.options.Orchestration),
			m.options.AutoSteps,
			m.options.PollInterval,
			m.ui.Theme,
			m.pendingOps,
		),
		width,
	)
	status := renderFixedStyledLine(m.styles.status, m.status, width)
	if m.lastError != "" {
		status = renderFixedStyledLine(m.styles.errorText, m.lastError, width)
	}
	if line := m.renderPendingStatusLine(); line != "" {
		status = renderFixedStyledLine(m.styles.subheader, truncatePlainText(" "+m.status+" | "+line, max(width-2, 1)), width)
		if m.lastError != "" {
			status = renderFixedStyledLine(m.styles.errorText, m.lastError, width)
		}
	}
	blocks := []string{title, meta, status}
	return lipgloss.JoinVertical(lipgloss.Left, blocks...)
}

func (m attachModel) renderInput() string {
	width := max(m.layoutMainWidth, 1)
	label := renderFixedStyledLine(m.styles.inputLabel, " message ", width)
	box := renderStaticPane(
		m.styles.inputBox,
		width,
		m.styles.inputBox.GetVerticalFrameSize()+1,
		m.renderInputValue(max(width-m.styles.inputBox.GetHorizontalFrameSize(), 1)),
	)
	assist := renderFixedStyledLine(m.styles.inputAssist, m.renderInputAssistText(), width)
	return lipgloss.JoinVertical(lipgloss.Left, label, box, assist)
}

func (m attachModel) renderInputValue(width int) string {
	if width < 1 {
		return ""
	}
	if m.input.Value() == "" {
		return m.styles.muted.Render(truncatePlainText(m.input.Placeholder, width))
	}
	value := m.input.Value()
	if m.input.Focused() {
		value += "|"
	}
	return truncateTailPlainText(value, width)
}

func (m attachModel) renderInputAssistText() string {
	if len(m.inputAssist.Suggestions) == 0 {
		return "Use / for commands, @ to mention agents, Tab to accept suggestions."
	}
	parts := make([]string, 0, len(m.inputAssist.Suggestions))
	for idx, suggestion := range m.inputAssist.Suggestions {
		label := suggestion.Label
		if idx == m.inputAssist.Selected {
			label = "[" + label + "]"
		}
		parts = append(parts, label)
	}
	prefix := "Commands"
	if m.inputAssist.Kind == attachInputAssistMention {
		prefix = "Mention"
	}
	return prefix + ": " + strings.Join(parts, "  ") + "  | Tab accept | Up/Down choose"
}

func (m attachModel) renderBody() string {
	main := renderStaticPane(
		m.styles.room,
		m.layoutRoomWidth,
		m.layoutRoomHeight,
		m.viewport.View(),
	)

	mainBlock := main
	if m.layoutPreviewWidth > 0 {
		preview := renderStaticPane(
			m.styles.preview,
			m.layoutPreviewWidth,
			m.layoutPreviewHeight,
			m.renderConversationPreviews(),
		)
		mainBlock = lipgloss.JoinHorizontal(lipgloss.Top, main, " ", preview)
	}

	if m.showSidebar {
		sidebar := renderStaticPane(
			m.styles.sidebar,
			m.layoutSidebarWidth,
			m.layoutSidebarHeight,
			m.renderSidebar(),
		)
		return lipgloss.JoinHorizontal(lipgloss.Top, mainBlock, " ", sidebar)
	}
	return mainBlock
}

func (m attachModel) renderFooter() string {
	return renderFixedStyledLine(
		m.styles.footer,
		"Enter send/accept | / commands | @ mentions | Up/Down history or assist | PgUp/PgDn scroll | [/ ] conversation | Tab accept or panes | Ctrl+L refresh",
		max(m.width, 1),
	)
}

func (m *attachModel) syncViewportContent(forceBottom bool) {
	if m.viewport.Width <= 0 || m.viewport.Height <= 0 {
		return
	}
	contentWidth := max(m.viewport.Width, 1)
	content := wrapRenderedText(m.renderConversationContent(m.roomConversationScope()), contentWidth)
	if content == m.lastViewportContent &&
		m.viewport.Width == m.lastViewportWidth &&
		m.viewport.Height == m.lastViewportHeight {
		if forceBottom && !m.viewport.AtBottom() {
			m.viewport.GotoBottom()
			m.stickyBottom = true
			return
		}
		return
	}
	offset := m.viewport.YOffset
	shouldPinBottom := forceBottom || m.stickyBottom
	m.viewport.SetContent(content)
	m.lastViewportContent = content
	m.lastViewportWidth = m.viewport.Width
	m.lastViewportHeight = m.viewport.Height
	if shouldPinBottom {
		m.viewport.GotoBottom()
		m.stickyBottom = true
		return
	}
	m.viewport.SetYOffset(offset)
	m.stickyBottom = m.viewport.AtBottom()
}

func (m attachModel) renderConversationContent(conversationID domain.ConversationID) string {
	events := m.displayEvents(conversationID)
	if len(events) == 0 {
		return m.styles.muted.Render("No messages yet. Type below to begin.")
	}
	blocks := make([]string, 0, len(events))
	for idx := 0; idx < len(events); {
		event := events[idx]
		if event.Kind != "message" {
			blocks = append(blocks, m.renderNonMessageBlock(event))
			idx++
			continue
		}
		group := []attachDisplayEvent{event}
		j := idx + 1
		for j < len(events) && canGroupDisplayEvents(group[len(group)-1], events[j]) {
			group = append(group, events[j])
			j++
		}
		blocks = append(blocks, m.renderMessageGroup(group))
		idx = j
	}
	if m.ui.CompactMessages {
		return strings.Join(blocks, "\n")
	}
	return strings.Join(blocks, "\n\n")
}

func canGroupDisplayEvents(a, b attachDisplayEvent) bool {
	return a.Kind == "message" &&
		b.Kind == "message" &&
		a.Sender == b.Sender &&
		a.ConversationID == b.ConversationID &&
		a.Pending == b.Pending
}

func (m attachModel) displayEvents(conversationID domain.ConversationID) []attachDisplayEvent {
	events := make([]attachDisplayEvent, 0, len(m.room.snapshot.Stream)+len(m.optimistic)+len(m.localNotices))
	replySummaryByID := buildReplySummaryIndex(m.room.snapshot.Messages)
	for _, entry := range m.room.snapshot.Stream {
		event, ok := m.streamEntryToDisplayEvent(entry, conversationID, replySummaryByID)
		if !ok {
			continue
		}
		events = append(events, event)
	}
	for _, pending := range m.optimistic {
		if conversationID != "" && pending.ConversationID != conversationID {
			continue
		}
		events = append(events, attachDisplayEvent{
			Kind:           "message",
			RecordedAt:     pending.SubmittedAt,
			ConversationID: pending.ConversationID,
			Sender:         pending.Sender,
			Body:           pending.Body,
			ToAgentIDs:     append([]domain.AgentID(nil), pending.ToAgentIDs...),
			Pending:        true,
		})
	}
	for _, notice := range m.localNotices {
		if conversationID != "" && notice.ConversationID != "" && notice.ConversationID != conversationID {
			continue
		}
		events = append(events, notice)
	}
	return events
}

func (m *attachModel) appendLocalNotice(event attachDisplayEvent) {
	m.localNotices = append(m.localNotices, event)
	if len(m.localNotices) > 20 {
		m.localNotices = m.localNotices[len(m.localNotices)-20:]
	}
}

func buildReplySummaryIndex(messages []domain.Message) map[domain.MessageID]string {
	index := make(map[domain.MessageID]string, len(messages))
	for _, message := range messages {
		if message.ID == "" {
			continue
		}
		index[message.ID] = fmt.Sprintf("%s: %s", senderNameForMessage(message), trimForSidebar(message.Body))
	}
	return index
}

func (m attachModel) streamEntryToDisplayEvent(
	entry runtimeadapter.StreamEntry,
	conversationID domain.ConversationID,
	replySummaryByID map[domain.MessageID]string,
) (attachDisplayEvent, bool) {
	switch event := entry.Payload.(type) {
	case application.SessionCreatedEvent:
		return attachDisplayEvent{
			Kind:       "system",
			RecordedAt: entry.RecordedAt,
			Body:       fmt.Sprintf("session created mode=%s status=%s", event.Session.Mode, event.Session.Status),
		}, true
	case application.SessionUpdatedEvent:
		return attachDisplayEvent{
			Kind:       "system",
			RecordedAt: entry.RecordedAt,
			Body:       fmt.Sprintf("session updated status=%s", event.Session.Status),
		}, true
	case application.MessageDispatchedEvent:
		if conversationID != "" && event.Message.ConversationID != conversationID {
			return attachDisplayEvent{}, false
		}
		return attachDisplayEvent{
			Kind:           "message",
			RecordedAt:     entry.RecordedAt,
			ConversationID: event.Message.ConversationID,
			Sender:         senderNameForMessage(event.Message),
			Body:           sanitizeLiveText(event.Message.Body),
			ReplyTo:        event.Message.ReplyTo,
			ReplySummary:   replySummaryByID[event.Message.ReplyTo],
			ToAgentIDs:     append([]domain.AgentID(nil), event.Message.ToAgentIDs...),
		}, true
	case application.AgentTaskCreatedEvent:
		if conversationID != "" && event.Task.ConversationID != conversationID {
			return attachDisplayEvent{}, false
		}
		return attachDisplayEvent{
			Kind:           "task",
			RecordedAt:     entry.RecordedAt,
			ConversationID: event.Task.ConversationID,
			Body:           formatTaskCreatedLine(event.Task, true),
		}, true
	case application.AgentTaskUpdatedEvent:
		if conversationID != "" && event.Task.ConversationID != conversationID {
			return attachDisplayEvent{}, false
		}
		return attachDisplayEvent{
			Kind:           "task",
			RecordedAt:     entry.RecordedAt,
			ConversationID: event.Task.ConversationID,
			Body:           formatTaskUpdatedLine(event.Task, true),
		}, true
	case application.AgentHandoffCreatedEvent:
		if conversationID != "" && event.Handoff.ConversationID != conversationID {
			return attachDisplayEvent{}, false
		}
		return attachDisplayEvent{
			Kind:           "task",
			RecordedAt:     entry.RecordedAt,
			ConversationID: event.Handoff.ConversationID,
			Body:           formatHandoffLine(event.Handoff, true),
		}, true
	default:
		return attachDisplayEvent{
			Kind:       "system",
			RecordedAt: entry.RecordedAt,
			Body:       entry.Topic,
		}, true
	}
}

func senderNameForMessage(message domain.Message) string {
	if message.Sender.ID != "" {
		return message.Sender.ID
	}
	return string(message.Sender.Type)
}

func (m attachModel) renderNonMessageBlock(event attachDisplayEvent) string {
	prefix := ""
	if m.ui.ShowTimestamps {
		prefix = m.styles.muted.Render(event.RecordedAt.UTC().Format("15:04:05")) + " "
	}
	switch event.Kind {
	case "task":
		return prefix + m.styles.task.Render(event.Body)
	case "status":
		return prefix + m.styles.statusBusy.Render(event.Body)
	default:
		return prefix + m.styles.system.Render(event.Body)
	}
}

func (m attachModel) renderMessageGroup(group []attachDisplayEvent) string {
	head := group[0]
	timestamp := ""
	if m.ui.ShowTimestamps {
		timestamp = m.styles.muted.Render(head.RecordedAt.UTC().Format("15:04:05")) + " "
	}
	senderStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(m.lookupAgentColor(head.Sender)))
	header := senderStyle.Render(head.Sender)
	if head.Pending {
		header = m.styles.pendingSender.Render("[pending] " + head.Sender)
	}
	if head.ConversationID != "" {
		header = m.styles.muted.Render("["+string(head.ConversationID)+"] ") + header
	}
	if head.ReplyTo != "" {
		header += m.styles.muted.Render(" ↩ " + string(head.ReplyTo))
	}
	if len(head.ToAgentIDs) > 0 {
		recipients := make([]string, 0, len(head.ToAgentIDs))
		for _, id := range head.ToAgentIDs {
			recipients = append(recipients, string(id))
		}
		header += m.styles.muted.Render(" -> " + strings.Join(recipients, ","))
	}
	if head.ReplySummary != "" {
		header += "\n" + m.styles.muted.Render("  in reply to "+head.ReplySummary)
	}
	bodies := make([]string, 0, len(group))
	for _, event := range group {
		bodies = append(bodies, m.styles.messageBody.Render(event.Body))
	}
	return timestamp + header + "\n" + strings.Join(bodies, "\n")
}

func (m attachModel) renderConversationPreviews() string {
	others := make([]domain.ConversationID, 0)
	if m.roomConversationScope() == "" {
		return m.styles.muted.Render("Session timeline already includes all conversations.")
	}
	for _, id := range m.room.conversations {
		if id == m.sendConversationID {
			continue
		}
		others = append(others, id)
	}
	if len(others) == 0 {
		return m.styles.muted.Render("No secondary conversations.")
	}

	sections := make([]string, 0, len(others))
	for _, id := range others {
		lines := m.previewConversationLines(id, 4)
		section := m.styles.sectionTitle.Render(string(id))
		if len(lines) == 0 {
			section += "\n" + m.styles.muted.Render("No messages")
		} else {
			section += "\n" + strings.Join(lines, "\n")
		}
		sections = append(sections, section)
	}
	return strings.Join(sections, "\n\n")
}

func (m attachModel) previewConversationLines(conversationID domain.ConversationID, limit int) []string {
	lines := make([]string, 0, limit)
	for _, message := range m.room.snapshot.Messages {
		if message.ConversationID != conversationID {
			continue
		}
		line := fmt.Sprintf("%s: %s", senderNameForMessage(message), trimForSidebar(message.Body))
		lines = append(lines, line)
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
}

func (m attachModel) renderSidebar() string {
	lines := []string{
		m.styles.sectionTitle.Render("Participants"),
	}
	lastSpeaker := m.lastAgentSpeaker()
	messageCounts, totalMessages := summarizeMessageCounts(m.room.snapshot.Messages)
	for _, agent := range m.agents {
		color := lipgloss.NewStyle().Foreground(lipgloss.Color(m.lookupAgentColor(string(agent.ID)))).Render(agent.Name)
		state := "idle"
		stateStyle := m.styles.muted
		if pending, exists := m.pendingAgentStates[agent.ID]; exists {
			state = pending
			if pending == "thinking" {
				stateStyle = m.styles.statusBusy
			} else {
				stateStyle = m.styles.statusWarn
			}
		} else if lastSpeaker == agent.ID {
			state = "last"
			stateStyle = m.styles.statusGood
		}
		lines = append(lines, fmt.Sprintf("%s  %s", color, stateStyle.Render(state)))
	}

	lines = append(lines, "")
	lines = append(lines, m.styles.sectionTitle.Render("Messages"))
	lines = append(lines, fmt.Sprintf("total: %d", totalMessages))
	for _, sender := range orderedSidebarMessageParticipants(m.agents, messageCounts) {
		count := messageCounts[sender]
		if count <= 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %d", sender, count))
	}

	lines = append(lines, "")
	lines = append(lines, m.styles.sectionTitle.Render("Runtime"))
	lines = append(lines, fmt.Sprintf("text: %s", formatProviderBindings(m.room.providers.TextGeneration)))
	lines = append(lines, fmt.Sprintf("sandbox: %s", formatProviderBindings(m.room.providers.SandboxedRuntimes)))
	lines = append(lines, fmt.Sprintf("vector: %s/%s", m.room.vectorBackend, m.room.vectorState.Status))
	if m.room.vectorState.LastRebuiltAt != nil {
		lines = append(lines, fmt.Sprintf("rebuild: %s", m.room.vectorState.LastRebuiltAt.UTC().Format("15:04:05")))
	}
	lines = append(lines, fmt.Sprintf("poll: %s", m.options.PollInterval))

	lines = append(lines, "")
	lines = append(lines, m.styles.sectionTitle.Render("Tasks"))
	pending, running, succeeded, failed := summarizeTasks(m.room.tasks)
	lines = append(lines, fmt.Sprintf("pending: %d", pending))
	lines = append(lines, fmt.Sprintf("running: %d", running))
	lines = append(lines, fmt.Sprintf("succeeded: %d", succeeded))
	lines = append(lines, fmt.Sprintf("failed: %d", failed))
	if latest := latestTask(m.room.tasks); latest != nil {
		lines = append(lines, fmt.Sprintf("latest: %s %s", latest.ID, latest.Status))
		lines = append(lines, fmt.Sprintf("owner: %s", latest.AssignedAgentID))
		lines = append(lines, trimForSidebar(latest.Instruction))
	}

	lines = append(lines, "")
	lines = append(lines, m.styles.sectionTitle.Render("Conversations"))
	lines = append(lines, fmt.Sprintf("view: %s", m.roomConversationLabel()))
	lines = append(lines, fmt.Sprintf("send: %s", m.sendConversationID))
	lines = append(lines, fmt.Sprintf("count: %d", len(m.room.conversations)))
	if m.canSplitConversations() {
		if m.splitPanes {
			lines = append(lines, "split: on")
		} else {
			lines = append(lines, "split: off")
		}
	}

	lines = append(lines, "")
	lines = append(lines, m.styles.sectionTitle.Render("Recent Input"))
	if len(m.inputHistory) == 0 {
		lines = append(lines, m.styles.muted.Render("none yet"))
	} else {
		start := len(m.inputHistory) - 5
		if start < 0 {
			start = 0
		}
		for _, item := range m.inputHistory[start:] {
			lines = append(lines, trimForSidebar(item))
		}
	}
	return strings.Join(lines, "\n")
}

func formatProviderBindings(bindings []runtimeadapter.ProviderBinding) string {
	if len(bindings) == 0 {
		return "none"
	}

	parts := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		part := binding.Name
		if !binding.Enabled {
			part += " (unconfigured)"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", ")
}

func (m attachModel) renderPendingStatusLine() string {
	if len(m.pendingAgentStates) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m.pendingAgentStates))
	for _, agent := range m.agents {
		state, exists := m.pendingAgentStates[agent.ID]
		if !exists {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s", agent.ID, state))
	}
	if len(parts) == 0 {
		return ""
	}
	return " activity: " + strings.Join(parts, "  |  ") + " "
}

func renderFixedStyledLine(style lipgloss.Style, text string, width int) string {
	if width < 1 {
		width = 1
	}
	contentWidth := max(width-style.GetHorizontalFrameSize(), 1)
	return style.
		Width(contentWidth).
		Height(1).
		MaxWidth(contentWidth).
		MaxHeight(1).
		Render(truncatePlainText(text, contentWidth))
}

func renderStaticPane(style lipgloss.Style, width, height int, content string) string {
	width = max(width, 1)
	height = max(height, 1)
	innerWidth := max(width-style.GetHorizontalFrameSize(), 1)
	innerHeight := max(height-style.GetVerticalFrameSize(), 1)
	contents := lipgloss.NewStyle().
		Width(innerWidth).
		Height(innerHeight).
		MaxWidth(innerWidth).
		MaxHeight(innerHeight).
		Render(content)
	return style.UnsetWidth().UnsetHeight().Render(contents)
}

func wrapRenderedText(content string, width int) string {
	if width < 1 {
		width = 1
	}
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Render(content)
}

func truncatePlainText(value string, limit int) string {
	if limit < 1 {
		return ""
	}
	value = strings.ReplaceAll(value, "\n", " ")
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	if limit == 1 {
		return "…"
	}
	runes := []rune(value)
	return string(runes[:limit-1]) + "…"
}

func truncateTailPlainText(value string, limit int) string {
	if limit < 1 {
		return ""
	}
	value = strings.ReplaceAll(value, "\n", " ")
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit == 1 {
		return "…"
	}
	return "…" + string(runes[len(runes)-limit+1:])
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func summarizeMessageCounts(messages []domain.Message) (map[string]int, int) {
	counts := make(map[string]int)
	total := 0
	for _, message := range messages {
		sender := senderNameForMessage(message)
		counts[sender]++
		total++
	}
	return counts, total
}

func orderedSidebarMessageParticipants(agents []domain.Agent, counts map[string]int) []string {
	ordered := make([]string, 0, len(counts))
	seen := make(map[string]struct{}, len(counts))
	if counts["operator"] > 0 {
		ordered = append(ordered, "operator")
		seen["operator"] = struct{}{}
	}
	for _, agent := range agents {
		id := string(agent.ID)
		if counts[id] <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		ordered = append(ordered, id)
		seen[id] = struct{}{}
	}
	for sender, count := range counts {
		if count <= 0 {
			continue
		}
		if _, exists := seen[sender]; exists {
			continue
		}
		ordered = append(ordered, sender)
	}
	return ordered
}

func summarizeTasks(tasks []application.SandboxTask) (pending, running, succeeded, failed int) {
	for _, task := range tasks {
		switch task.Status {
		case application.SandboxTaskStatusPending:
			pending++
		case application.SandboxTaskStatusRunning:
			running++
		case application.SandboxTaskStatusSucceeded:
			succeeded++
		case application.SandboxTaskStatusFailed:
			failed++
		}
	}
	return
}

func latestTask(tasks []application.SandboxTask) *application.SandboxTask {
	if len(tasks) == 0 {
		return nil
	}
	latest := tasks[0]
	for _, task := range tasks[1:] {
		if task.CreatedAt.After(latest.CreatedAt) {
			latest = task
		}
	}
	return &latest
}

func (m attachModel) lastAgentSpeaker() domain.AgentID {
	messages := activeConversationMessages(m.room.snapshot.Messages, m.roomConversationScope())
	if len(messages) == 0 {
		return ""
	}
	last := messages[len(messages)-1]
	if last.Sender.Type != domain.MessageSenderTypeAgent {
		return ""
	}
	return domain.AgentID(last.Sender.ID)
}

func trimForSidebar(value string) string {
	value = sanitizeLiveText(value)
	if len(value) <= 26 {
		return value
	}
	return value[:23] + "..."
}

func (m attachModel) lookupAgentColor(agentID string) string {
	if color, exists := m.ui.AgentColors[agentID]; exists {
		return color
	}
	if color, exists := m.agentColors[agentID]; exists {
		return color
	}
	switch agentID {
	case "operator":
		return "#f97316"
	case "system":
		return "#fbbf24"
	case "task":
		return "#a78bfa"
	}

	hash := fnv.New64a()
	_, _ = hash.Write([]byte(agentID))
	var seed [8]byte
	binary.LittleEndian.PutUint64(seed[:], m.colorSeed)
	_, _ = hash.Write(seed[:])
	value := hash.Sum64()
	r := 96 + int(value&0x5f)
	g := 96 + int((value>>8)&0x5f)
	b := 96 + int((value>>16)&0x5f)
	color := fmt.Sprintf("#%02x%02x%02x", r, g, b)
	m.agentColors[agentID] = color
	return color
}

func (m attachModel) canSplitConversations() bool {
	return m.roomConversationScope() != "" && len(m.room.conversations) > 1 && m.layoutMainWidth > 90
}

func (m attachModel) activeConversation() domain.ConversationID {
	if m.options.ConversationID != "" {
		return m.options.ConversationID
	}
	if m.selectedConvID != "" {
		return m.selectedConvID
	}
	return m.sendConversationID
}

func (m attachModel) roomConversationScope() domain.ConversationID {
	if m.options.ConversationID != "" {
		return m.options.ConversationID
	}
	return ""
}

func (m attachModel) roomConversationLabel() string {
	if scope := m.roomConversationScope(); scope != "" {
		return string(scope)
	}
	return "session"
}

func attachFetchRoomStateCmd(ctx context.Context, rt *runtimeadapter.Runtime, sessionID domain.SessionID) tea.Cmd {
	return func() tea.Msg {
		state, err := loadAttachRoomState(ctx, rt, sessionID)
		if err != nil {
			return attachErrMsg{err: err}
		}
		return attachRoomStateMsg{state: state}
	}
}

func loadAttachRoomState(ctx context.Context, rt *runtimeadapter.Runtime, sessionID domain.SessionID) (attachRoomState, error) {
	snapshot, err := rt.InspectSession(ctx, sessionID)
	if err != nil {
		return attachRoomState{}, err
	}
	tasks, err := rt.ListSandboxTasksBySession(ctx, application.ListSandboxTasksQuery{SessionID: sessionID})
	if err != nil {
		return attachRoomState{}, err
	}
	vectorState, vectorBackend, err := rt.VectorStatus(ctx, application.VectorStatusQuery{SessionID: sessionID})
	if err != nil {
		return attachRoomState{}, err
	}
	conversations := collectConversations(snapshot.Messages)
	activeTasks := make(map[application.AgentTaskID]application.SandboxTask, len(tasks))
	for _, task := range tasks {
		activeTasks[task.ID] = task
	}
	return attachRoomState{
		snapshot:       snapshot,
		tasks:          tasks,
		vectorState:    vectorState,
		vectorBackend:  vectorBackend,
		providers:      rt.ProviderCatalog(),
		conversations:  conversations,
		activeTaskByID: activeTasks,
	}, nil
}

func collectConversations(messages []domain.Message) []domain.ConversationID {
	seen := make(map[domain.ConversationID]struct{}, len(messages))
	conversations := make([]domain.ConversationID, 0)
	for _, message := range messages {
		if message.ConversationID == "" {
			continue
		}
		if _, exists := seen[message.ConversationID]; exists {
			continue
		}
		seen[message.ConversationID] = struct{}{}
		conversations = append(conversations, message.ConversationID)
	}
	return conversations
}

func attachDispatchCmd(
	ctx context.Context,
	rt *runtimeadapter.Runtime,
	sessionID domain.SessionID,
	conversationID domain.ConversationID,
	request attachDispatchRequest,
	autoSteps int,
) tea.Cmd {
	return func() tea.Msg {
		channel, policy := attachDispatchRouting(request.ToAgentIDs)
		if _, err := rt.DispatchMessage(ctx, application.DispatchMessageCommand{
			SessionID:      sessionID,
			ConversationID: conversationID,
			Sender:         domain.UserSender("operator"),
			ToAgentIDs:     append([]domain.AgentID(nil), request.ToAgentIDs...),
			Channel:        channel,
			Kind:           domain.MessageKindUtterance,
			Body:           request.Body,
			Policy:         policy,
		}); err != nil {
			return attachErrMsg{err: err}
		}
		state, err := loadAttachRoomState(ctx, rt, sessionID)
		if err != nil {
			return attachErrMsg{err: err}
		}
		return attachDispatchCompleteMsg{
			state:     state,
			request:   request,
			autoSteps: autoSteps,
		}
	}
}

func attachRunStepCmd(
	ctx context.Context,
	rt *runtimeadapter.Runtime,
	sessionID domain.SessionID,
	conversationID domain.ConversationID,
	mode application.OrchestrationMode,
	replyRouting application.ReplyRoutingMode,
	remaining int,
) tea.Cmd {
	return func() tea.Msg {
		step, err := rt.StepSession(ctx, application.StepSessionCommand{
			SessionID:         sessionID,
			ConversationID:    conversationID,
			OrchestrationMode: mode,
			ReplyRoutingMode:  replyRouting,
		})
		if err != nil {
			return attachErrMsg{err: err}
		}
		state, err := loadAttachRoomState(ctx, rt, sessionID)
		if err != nil {
			return attachErrMsg{err: err}
		}
		return attachStepProgressMsg{
			state:     state,
			step:      step,
			remaining: remaining,
		}
	}
}

func attachTickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return attachTickMsg(t)
	})
}

func attachBeginDispatchTickCmd(request attachDispatchRequest, autoSteps int) tea.Cmd {
	return tea.Tick(10*time.Millisecond, func(time.Time) tea.Msg {
		return attachBeginDispatchMsg{
			request:   request,
			autoSteps: autoSteps,
		}
	})
}

func attachContinueAutoTickCmd(remaining int) tea.Cmd {
	return tea.Tick(10*time.Millisecond, func(time.Time) tea.Msg {
		return attachContinueAutoMsg{remaining: remaining}
	})
}
