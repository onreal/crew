package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"crew/internal/domain"
)

func (m attachModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = typed.Width
		m.height = typed.Height
		m.layout()
		m.syncViewportContent(false)
		return m, nil
	case attachRoomStateMsg:
		streamCountBefore := m.printedStreamCount
		m.room = typed.state
		m.ensureActiveConversation()
		m.lastError = ""
		m.syncViewportContent(false)
		m.printedStreamCount = len(m.room.snapshot.Stream)
		return m, tea.Batch(m.printHeaderIfChangedCmd(), m.printNewStreamEntriesCmd(streamCountBefore))
	case attachDispatchCompleteMsg:
		streamCountBefore := m.printedStreamCount
		m.room = typed.state
		m.ensureActiveConversation()
		m.popOptimistic(typed.request.ID)
		m.lastError = ""
		m.status = "operator message sent"
		cmds := []tea.Cmd{}
		if typed.autoSteps > 0 {
			m.pendingOps = 1
			m.setPendingSequence(typed.autoSteps)
			m.status = fmt.Sprintf("operator message sent, auto running %d turn(s)", typed.autoSteps)
			m.syncViewportContent(false)
			m.printedStreamCount = len(m.room.snapshot.Stream)
			cmds = append(cmds, m.printHeaderIfChangedCmd(), m.printNewStreamEntriesCmd(streamCountBefore), attachContinueAutoTickCmd(typed.autoSteps))
			return m, tea.Batch(cmds...)
		}
		m.syncViewportContent(false)
		m.printedStreamCount = len(m.room.snapshot.Stream)
		return m, tea.Batch(m.printHeaderIfChangedCmd(), m.printNewStreamEntriesCmd(streamCountBefore))
	case attachStepStreamStartedMsg:
		m.activeStepEvents = typed.events
		return m, attachAwaitStepEventCmd(typed.events)
	case attachProgressMsg:
		if typed.event.AgentID != "" && typed.event.Text != "" {
			m.pendingAgentStates[typed.event.AgentID] = typed.event.Kind
			if strings.TrimSpace(typed.event.Kind) == "" {
				m.pendingAgentStates[typed.event.AgentID] = "progress"
			}
			m.progressByAgent[typed.event.AgentID] = typed.event
			m.status = string(typed.event.AgentID) + " is " + displayProgressKind(typed.event.Kind)
			if m.options.Reasoning {
				m.syncViewportContent(false)
			}
		}
		if m.activeStepEvents != nil {
			return m, attachAwaitStepEventCmd(m.activeStepEvents)
		}
		return m, nil
	case attachStepProgressMsg:
		streamCountBefore := m.printedStreamCount
		reasoningCountBefore := m.printedReasoningCount
		m.activeStepEvents = nil
		m.room = typed.state
		m.ensureActiveConversation()
		m.lastError = ""
		hadReasoning := len(m.progressByAgent) > 0
		m.commitProgressHistory()
		cmds := []tea.Cmd{}
		if typed.remaining > 1 && typed.step.Stepped {
			m.pendingOps = 1
			m.setPendingSequence(typed.remaining - 1)
			if typed.step.Agent != nil {
				m.status = fmt.Sprintf("%s replied, continuing auto run (%d left)", typed.step.Agent.ID, typed.remaining-1)
			} else {
				m.status = fmt.Sprintf("continuing auto run (%d left)", typed.remaining-1)
			}
			m.syncViewportContent(false)
			m.printedStreamCount = len(m.room.snapshot.Stream)
			m.printedReasoningCount = len(m.progressHistory)
			cmds = append(cmds, m.printHeaderIfChangedCmd(), m.printNewStreamEntriesCmd(streamCountBefore), m.printNewReasoningEntriesCmd(reasoningCountBefore), attachContinueAutoTickCmd(typed.remaining-1))
			return m, tea.Batch(cmds...)
		}
		m.pendingOps = 0
		clear(m.pendingAgentStates)
		clear(m.progressByAgent)
		m.status = fmt.Sprintf("step=%t reason=%s", typed.step.Stepped, typed.step.Reason)
		if typed.step.Agent != nil {
			m.status = fmt.Sprintf("step agent=%s", typed.step.Agent.ID)
			if !hadReasoning {
				m.status += " (no reasoning emitted)"
			}
		}
		if !typed.step.Stepped && typed.step.Reason != "" {
			m.status = fmt.Sprintf("stopped: %s", typed.step.Reason)
		}
		m.syncViewportContent(false)
		m.printedStreamCount = len(m.room.snapshot.Stream)
		m.printedReasoningCount = len(m.progressHistory)
		return m, tea.Batch(m.printHeaderIfChangedCmd(), m.printNewStreamEntriesCmd(streamCountBefore), m.printNewReasoningEntriesCmd(reasoningCountBefore))
	case attachErrMsg:
		m.activeStepEvents = nil
		m.pendingOps = 0
		clear(m.pendingAgentStates)
		clear(m.progressByAgent)
		m.lastError = typed.err.Error()
		m.status = "room error"
		m.syncViewportContent(false)
		return m, m.printHeaderIfChangedCmd()
	case attachTickMsg:
		m.spinnerFrame++
		return m, tea.Batch(
			attachFetchRoomStateCmd(m.ctx, m.rt, m.options.SessionID),
			attachTickCmd(m.options.PollInterval),
		)
	case attachBeginDispatchMsg:
		return m, attachDispatchCmd(m.ctx, m.rt, m.options.SessionID, m.sendConversationID, typed.request, typed.autoSteps)
	case attachContinueAutoMsg:
		return m, attachRunStepCmd(m.ctx, m.rt, m.options.SessionID, m.sendConversationID, m.options.Orchestration, m.options.ReplyRouting, typed.remaining)
	case tea.KeyMsg:
		if handled, cmd := m.handleKeyMsg(typed); handled {
			return m, cmd
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.refreshInputAssist()
	return m, cmd
}

func (m *attachModel) handleKeyMsg(msg tea.KeyMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc":
		return true, tea.Quit
	case "ctrl+y":
		m.copyCurrentTUISnapshot()
		return true, nil
	case "enter":
		if m.acceptSelectedInputAssist(false) {
			return true, nil
		}
		value := strings.TrimSpace(m.input.Value())
		if value == "" {
			return true, nil
		}
		m.recordHistory(value)
		m.input.SetValue("")
		m.refreshInputAssist()
		return true, m.submitInput(value)
	case "ctrl+l":
		return true, attachFetchRoomStateCmd(m.ctx, m.rt, m.options.SessionID)
	case "up":
		if m.selectPreviousInputAssist() {
			return true, nil
		}
		m.historyUp()
		return true, nil
	case "down":
		if m.selectNextInputAssist() {
			return true, nil
		}
		m.historyDown()
		return true, nil
	case "ctrl+u":
		m.input.SetValue("")
		m.refreshInputAssist()
		return true, nil
	case "ctrl+d":
		m.showRecentInputStatus()
		return true, nil
	case "tab":
		if m.acceptSelectedInputAssist(true) {
			return true, nil
		}
		return true, nil
	case "shift+tab":
		if m.selectPreviousInputAssist() {
			return true, nil
		}
		return true, nil
	case "]":
		if m.options.ConversationID == "" {
			m.cycleConversation(1)
		}
		return true, nil
	case "[":
		if m.options.ConversationID == "" {
			m.cycleConversation(-1)
		}
		return true, nil
	}
	return false, nil
}

func (m *attachModel) submitInput(value string) tea.Cmd {
	if strings.HasPrefix(value, "/") {
		return m.handleCommand(value)
	}

	request := m.newAttachDispatchRequest(value)
	channel, _ := attachDispatchRouting(request.ToAgentIDs)
	effectiveAutoSteps := effectiveAttachAutoSteps(m.options.AutoSteps, request.ToAgentIDs)
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
	m.syncViewportContent(false)
	return attachBeginDispatchTickCmd(request, effectiveAutoSteps)
}

func (m attachModel) printNewStreamEntriesCmd(from int) tea.Cmd {
	if from < 0 {
		from = 0
	}
	if from >= len(m.room.snapshot.Stream) {
		return nil
	}
	replySummaryByID := buildReplySummaryIndex(m.room.snapshot.Messages)
	events := make([]attachDisplayEvent, 0, len(m.room.snapshot.Stream)-from)
	for _, entry := range m.room.snapshot.Stream[from:] {
		event, ok := m.streamEntryToDisplayEvent(entry, m.roomConversationScope(), replySummaryByID)
		if !ok {
			continue
		}
		events = append(events, event)
	}
	if len(events) == 0 {
		return nil
	}
	rendered := strings.TrimSpace(m.renderPlainDisplayEvents(events))
	if rendered == "" {
		return nil
	}
	return tea.Printf("%s", rendered)
}

func (m *attachModel) printHeaderIfChangedCmd() tea.Cmd {
	rendered := strings.TrimSpace(m.renderHeader())
	if rendered == "" || rendered == m.lastPrintedHeader {
		return nil
	}
	m.lastPrintedHeader = rendered
	return tea.Printf("%s", rendered)
}

func (m attachModel) printNewReasoningEntriesCmd(from int) tea.Cmd {
	if !m.options.Reasoning {
		return nil
	}
	if from < 0 {
		from = 0
	}
	if from >= len(m.progressHistory) {
		return nil
	}
	events := append([]attachDisplayEvent(nil), m.progressHistory[from:]...)
	rendered := strings.TrimSpace(m.renderPlainDisplayEvents(events))
	if rendered == "" {
		return nil
	}
	return tea.Printf("%s", rendered)
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

func (m *attachModel) handleCommand(raw string) tea.Cmd {
	fields := strings.Fields(raw)
	switch fields[0] {
	case "/quit", "/exit":
		return tea.Quit
	case "/help":
		m.status = interactiveHelpText(m.options.AutoSteps)
		return nil
	case "/step":
		m.pendingOps = 1
		m.setPendingSequence(1)
		m.status = "running one agent turn..."
		m.syncViewportContent(false)
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
		m.pendingOps = 1
		m.setPendingSequence(maxSteps)
		m.status = fmt.Sprintf("running auto for %d turn(s)...", maxSteps)
		m.syncViewportContent(false)
		return attachRunStepCmd(m.ctx, m.rt, m.options.SessionID, m.sendConversationID, m.options.Orchestration, m.options.ReplyRouting, maxSteps)
	default:
		return func() tea.Msg {
			return attachErrMsg{err: newCLIError("invalid_arguments", fmt.Sprintf("unknown interactive command %q", fields[0]))}
		}
	}
}
