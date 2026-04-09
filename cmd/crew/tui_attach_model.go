package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	codexadapter "crew/internal/adapters/providers/codex"
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
	ctx                   context.Context
	rt                    *runtimeadapter.Runtime
	options               liveViewOptions
	ui                    platform.UIConfig
	clipboard             attachClipboard
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
	reasoningByAgent      map[domain.AgentID]string
	activeStepEvents      <-chan tea.Msg
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
	layoutArtworkWidth    int
	layoutArtworkHeight   int
	layoutSidebarWidth    int
	layoutSidebarHeight   int
	lastViewportContent   string
	lastRoomPlainContent  string
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

type attachReasoningMsg struct {
	agentID domain.AgentID
	text    string
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
	inputWrap, inputBox, sidebar, room, preview             lipgloss.Style
	footer, muted, system, task, pendingSender              lipgloss.Style
	messageBody, blockHeader, sectionTitle                  lipgloss.Style
	statusGood, statusWarn, statusBusy, inputAssist         lipgloss.Style
	artworkDots, artworkAlert, artworkBrand                 lipgloss.Style
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
		clipboard:          newAttachClipboard(nil),
		agentsDir:          options.AgentsDir,
		sendConversationID: sendConversationID,
		selectedConvID:     sendConversationID,
		splitPanes:         ui.AttachSplitPanes,
		agentColors:        make(map[string]string),
		colorSeed:          uint64(time.Now().UnixNano()),
		styles:             newAttachStyles(ui),
		input:              input,
		viewport:           viewport.New(0, 0),
		stickyBottom:       true,
		historyIndex:       -1,
		status:             fmt.Sprintf("attached to %s / %s", options.SessionID, sendConversationID),
		pendingAgentStates: make(map[domain.AgentID]string),
		reasoningByAgent:   make(map[domain.AgentID]string),
	}
	model.agents, model.agentColors = mustLoadAttachAgents(model.agentsDir)
	model.refreshInputAssist()
	model.viewport.Style = model.styles.room
	model.viewport.SetContent(model.styles.muted.Render("Loading room..."))
	model.lastRoomPlainContent = "Loading room..."
	return model
}

func newAttachReasoningMsg(event codexadapter.ReasoningEvent) attachReasoningMsg {
	return attachReasoningMsg{agentID: domain.AgentID(event.AgentID), text: strings.TrimSpace(event.Text)}
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
	bg, panel, border := "#1c1917", "#292524", "#57534e"
	text, muted, accent := "#f5f5f4", "#d6d3d1", "#f97316"
	good, warn, busy := "#34d399", "#fbbf24", "#38bdf8"
	artworkDots, artworkBrand := "#78716c", "#f4e7cf"
	if ui.Theme == "graphite" {
		bg, panel, border = "#111827", "#1f2937", "#4b5563"
		text, muted, accent = "#f9fafb", "#9ca3af", "#38bdf8"
		good, warn, busy = "#22c55e", "#f59e0b", "#60a5fa"
		artworkDots, artworkBrand = "#6b7280", "#f5ead7"
	}

	return attachStyles{
		frame:         lipgloss.NewStyle().Background(lipgloss.Color(bg)).Foreground(lipgloss.Color(text)),
		header:        lipgloss.NewStyle().Foreground(lipgloss.Color(text)).Background(lipgloss.Color(panel)).Bold(true).Padding(0, 1),
		subheader:     lipgloss.NewStyle().Foreground(lipgloss.Color(muted)).Background(lipgloss.Color(panel)).Padding(0, 1),
		status:        lipgloss.NewStyle().Foreground(lipgloss.Color(accent)).Bold(true),
		errorText:     lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444")).Bold(true),
		inputLabel:    lipgloss.NewStyle().Foreground(lipgloss.Color(accent)).Bold(true),
		inputWrap:     lipgloss.NewStyle().Background(lipgloss.Color(panel)).Padding(1, 0, 0, 0),
		inputBox:      lipgloss.NewStyle().Background(lipgloss.Color(bg)).Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color(border)).Padding(1, 0),
		sidebar:       lipgloss.NewStyle().Foreground(lipgloss.Color(muted)),
		room:          lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color(border)).Padding(0, 0),
		preview:       lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color(border)).Padding(0, 0),
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
		inputAssist:   lipgloss.NewStyle().Foreground(lipgloss.Color(muted)).Background(lipgloss.Color(panel)),
		artworkDots:   lipgloss.NewStyle().Foreground(lipgloss.Color(artworkDots)),
		artworkAlert:  lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444")).Bold(true),
		artworkBrand:  lipgloss.NewStyle().Foreground(lipgloss.Color(artworkBrand)).Bold(true),
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
	return m.styles.frame.
		Width(m.width).
		Height(m.height).
		Render(lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), m.renderBody(), m.renderInput(), m.renderFooter()))
}
