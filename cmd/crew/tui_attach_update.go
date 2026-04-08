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
		if handled, cmd := m.handleKeyMsg(typed); handled {
			return m, cmd
		}
	}

	var cmd, vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	m.stickyBottom = m.viewport.AtBottom()
	m.input, cmd = m.input.Update(msg)
	m.refreshInputAssist()
	return m, tea.Batch(vpCmd, cmd)
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
	case "pgup":
		m.viewport.HalfViewUp()
		m.stickyBottom = m.viewport.AtBottom()
		return true, nil
	case "pgdown":
		m.viewport.HalfViewDown()
		m.stickyBottom = m.viewport.AtBottom()
		return true, nil
	case "home":
		m.viewport.GotoTop()
		m.stickyBottom = false
		return true, nil
	case "end":
		m.viewport.GotoBottom()
		m.stickyBottom = true
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
