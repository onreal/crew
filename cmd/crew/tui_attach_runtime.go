package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	runtimeadapter "crew/internal/adapters/runtime"
	"crew/internal/application"
	"crew/internal/domain"
)

func (s *runtimeState) runTUISessionView(ctx context.Context, in io.Reader, out io.Writer, options liveViewOptions) error {
	if options.TerminalScrollback || !supportsFullScreenTUI(in, out) {
		err := s.runInteractiveSessionView(ctx, in, out, options)
		if err == nil {
			return printAttachResumeHint(out, options)
		}
		return err
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
		model.clipboard = newAttachClipboard(out)
		program := tea.NewProgram(
			model,
			tea.WithInput(in),
			tea.WithOutput(out),
		)
		_, err = program.Run()
		if err == nil {
			return nil, printAttachResumeHint(out, options)
		}
		return nil, err
	})
	return err
}

func printAttachResumeHint(out io.Writer, options liveViewOptions) error {
	if !options.Follow {
		return nil
	}
	_, err := fmt.Fprintf(out, "\nTo resume this session: %s\n", attachResumeCommand(options))
	return err
}

func attachResumeCommand(options liveViewOptions) string {
	command := fmt.Sprintf("crew tui attach --session-id %s", options.SessionID)
	if options.ConversationID != "" {
		command += " --conversation-id " + string(options.ConversationID)
	}
	if options.TerminalScrollback {
		command += " --terminal-scrollback"
	}
	if options.Debug {
		command += " --debug"
	}
	if options.Reasoning {
		command += " --reasoning"
	}
	return command
}

func supportsFullScreenTUI(in io.Reader, out io.Writer) bool {
	inFile, inOK := in.(*os.File)
	outFile, outOK := out.(*os.File)
	if !inOK || !outOK {
		return false
	}
	return term.IsTerminal(int(inFile.Fd())) && term.IsTerminal(int(outFile.Fd()))
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
		conversations:  collectConversations(snapshot.Messages),
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
		return attachDispatchCompleteMsg{state: state, request: request, autoSteps: autoSteps}
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
		events := make(chan tea.Msg, 32)
		go func() {
			defer close(events)
			stepCtx := application.WithTransientProgressReporter(ctx, func(event application.TransientProgressEvent) {
				msg := newAttachProgressMsg(event)
				if msg.event.AgentID == "" || msg.event.Text == "" {
					return
				}
				select {
				case events <- msg:
				default:
				}
			})
			step, err := rt.StepSession(stepCtx, application.StepSessionCommand{
				SessionID:         sessionID,
				ConversationID:    conversationID,
				OrchestrationMode: mode,
				ReplyRoutingMode:  replyRouting,
			})
			if err != nil {
				events <- attachErrMsg{err: err}
				return
			}
			state, err := loadAttachRoomState(ctx, rt, sessionID)
			if err != nil {
				events <- attachErrMsg{err: err}
				return
			}
			events <- attachStepProgressMsg{state: state, step: step, remaining: remaining}
		}()
		return attachStepStreamStartedMsg{events: events}
	}
}

func attachAwaitStepEventCmd(events <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-events
		if !ok {
			return nil
		}
		return msg
	}
}

func attachTickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return attachTickMsg(t)
	})
}

func attachBeginDispatchTickCmd(request attachDispatchRequest, autoSteps int) tea.Cmd {
	return tea.Tick(10*time.Millisecond, func(time.Time) tea.Msg {
		return attachBeginDispatchMsg{request: request, autoSteps: autoSteps}
	})
}

func attachContinueAutoTickCmd(remaining int) tea.Cmd {
	return tea.Tick(10*time.Millisecond, func(time.Time) tea.Msg {
		return attachContinueAutoMsg{remaining: remaining}
	})
}
