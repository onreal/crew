package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	runtimeadapter "crew/internal/adapters/runtime"
	"crew/internal/application"
	"crew/internal/domain"
	"crew/internal/platform"
)

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
	ctx                  context.Context
	rt                   *runtimeadapter.Runtime
	options              liveViewOptions
	ui                   platform.UIConfig
	clipboard            attachClipboard
	agentsDir            string
	sendConversationID   domain.ConversationID
	agents               []domain.Agent
	agentColors          map[string]string
	colorSeed            uint64
	styles               attachStyles
	input                textinput.Model
	inputAssist          attachInputAssist
	room                 attachRoomState
	optimistic           []optimisticMessage
	localNotices         []attachDisplayEvent
	inputHistory         []string
	historyIndex         int
	historyDraft         string
	nextOptimisticID     int
	status               string
	lastError            string
	pendingOps           int
	pendingAgentStates   map[domain.AgentID]string
	progressByAgent      map[domain.AgentID]application.TransientProgressEvent
	progressHistory      []attachDisplayEvent
	activeStepEvents     <-chan tea.Msg
	spinnerFrame         int
	printedStreamCount   int
	printedReasoningCount int
	lastPrintedHeader    string
	width                int
	height               int
	layoutMainWidth      int
	layoutBodyHeight     int
	lastRoomContent      string
	lastRoomPlainContent string
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
	ProgressKind   string
	Pending        bool
}

type attachRoomStateMsg struct{ state attachRoomState }

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

type attachProgressMsg struct {
	event application.TransientProgressEvent
}

type attachStepStreamStartedMsg struct{ events <-chan tea.Msg }
type attachErrMsg struct{ err error }
type attachTickMsg time.Time
type attachBeginDispatchMsg struct {
	request   attachDispatchRequest
	autoSteps int
}
type attachContinueAutoMsg struct{ remaining int }

type attachDispatchRequest struct {
	ID         string
	Body       string
	ToAgentIDs []domain.AgentID
}

type attachStyles struct {
	frame, header, subheader, status, errorText, inputLabel lipgloss.Style
	footer, muted, system, task, pendingSender              lipgloss.Style
	messageBody, sectionTitle                               lipgloss.Style
	statusBusy, inputAssist, inputBox, statusLine           lipgloss.Style
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
	input.Prompt = "> "
	input.Placeholder = "Type a message or /help"
	input.CharLimit = 4096
	input.Focus()

	model := attachModel{
		ctx:                ctx,
		rt:                 rt,
		options:            options,
		ui:                 ui,
		clipboard:          newAttachClipboard(nil),
		agentsDir:          options.AgentsDir,
		sendConversationID: sendConversationID,
		agentColors:        make(map[string]string),
		colorSeed:          uint64(time.Now().UnixNano()),
		styles:             newAttachStyles(ui),
		input:              input,
		historyIndex:       -1,
		status:             fmt.Sprintf("attached to %s / %s", options.SessionID, sendConversationID),
		pendingAgentStates: make(map[domain.AgentID]string),
		progressByAgent:    make(map[domain.AgentID]application.TransientProgressEvent),
	}
	model.agents, model.agentColors = mustLoadAttachAgents(model.agentsDir)
	model.refreshInputAssist()
	model.lastRoomContent = model.styles.muted.Render("Loading room...")
	model.lastRoomPlainContent = "Loading room..."
	return model
}

func newAttachProgressMsg(event application.TransientProgressEvent) attachProgressMsg {
	event.Text = strings.TrimSpace(event.Text)
	return attachProgressMsg{event: event}
}

func mustLoadAttachAgents(agentsDir string) ([]domain.Agent, map[string]string) {
	if strings.TrimSpace(agentsDir) == "" {
		resolved, err := resolveSelectedAgentsDir("")
		if err != nil {
			return nil, map[string]string{}
		}
		agentsDir = resolved
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
	panel := "#292524"
	text, muted, accent := "#f5f5f4", "#d6d3d1", "#f97316"
	busy := "#38bdf8"
	if ui.Theme == "graphite" {
		panel = "#1f2937"
		text, muted, accent = "#f9fafb", "#9ca3af", "#38bdf8"
		busy = "#60a5fa"
	}

	return attachStyles{
		frame:         lipgloss.NewStyle().Foreground(lipgloss.Color(text)),
		header:        lipgloss.NewStyle().Foreground(lipgloss.Color(text)).Bold(true),
		subheader:     lipgloss.NewStyle().Foreground(lipgloss.Color(muted)),
		status:        lipgloss.NewStyle().Foreground(lipgloss.Color(accent)).Bold(true),
		errorText:     lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444")).Bold(true),
		inputLabel:    lipgloss.NewStyle().Foreground(lipgloss.Color(accent)).Bold(true),
		inputBox:      lipgloss.NewStyle().Background(lipgloss.Color(panel)),
		footer:        lipgloss.NewStyle().Foreground(lipgloss.Color(muted)),
		muted:         lipgloss.NewStyle().Foreground(lipgloss.Color(muted)),
		system:        lipgloss.NewStyle().Foreground(lipgloss.Color("#fbbf24")),
		task:          lipgloss.NewStyle().Foreground(lipgloss.Color("#a78bfa")),
		pendingSender: lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8")).Italic(true),
		messageBody:   lipgloss.NewStyle().PaddingLeft(2),
		sectionTitle:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(accent)),
		statusBusy:    lipgloss.NewStyle().Foreground(lipgloss.Color(busy)).Bold(true),
		inputAssist:   lipgloss.NewStyle().Foreground(lipgloss.Color(muted)).Background(lipgloss.Color(panel)),
		statusLine:    lipgloss.NewStyle().Foreground(lipgloss.Color(muted)),
	}
}

func (m attachModel) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		attachFetchRoomStateCmd(m.ctx, m.rt, m.options.SessionID),
		attachTickCmd(m.options.PollInterval),
	)
}

func (m attachModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading crew TUI..."
	}
	return m.styles.frame.Width(m.layoutMainWidth).Render(lipgloss.JoinVertical(
		lipgloss.Left,
		m.renderBody(),
		m.renderInput(),
		m.renderFooter(),
	))
}
