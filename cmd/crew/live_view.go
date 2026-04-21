package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	runtimeadapter "crew/internal/adapters/runtime"
	"crew/internal/application"
	"crew/internal/domain"
)

type liveViewOptions struct {
	SessionID          domain.SessionID
	ConversationID     domain.ConversationID
	AgentsDir          string
	Follow             bool
	TerminalScrollback bool
	PollInterval       time.Duration
	PrintHeader        bool
	AutoSteps          int
	Orchestration      application.OrchestrationMode
	ReplyRouting       application.ReplyRoutingMode
	Debug              bool
	Reasoning          bool
}

func (s *runtimeState) runLiveSessionView(ctx context.Context, out io.Writer, options liveViewOptions) error {
	if options.PollInterval <= 0 {
		if err := s.bootstrap(); err != nil {
			return err
		}
		options.PollInterval = time.Duration(s.loaded.Config.UI.RefreshIntervalMillis) * time.Millisecond
		if options.PollInterval <= 0 {
			options.PollInterval = 250 * time.Millisecond
		}
	}

	if options.PrintHeader {
		if _, err := fmt.Fprintln(out, formatLiveViewHeader(options)); err != nil {
			return err
		}
	}

	_, err := s.withLocalRuntime(ctx, false, func(rt *runtimeadapter.Runtime) (any, error) {
		seen := 0
		for {
			snapshot, err := rt.InspectSession(ctx, options.SessionID)
			if err != nil {
				return nil, err
			}

			if seen > len(snapshot.Stream) {
				seen = len(snapshot.Stream)
			}

			for _, entry := range snapshot.Stream[seen:] {
				line, ok := formatStreamEntry(entry, options.ConversationID)
				if !ok {
					continue
				}
				if _, err := fmt.Fprintln(out, line); err != nil {
					return nil, err
				}
			}
			seen = len(snapshot.Stream)

			if !options.Follow {
				return nil, nil
			}

			if err := sleepContext(ctx, options.PollInterval); err != nil {
				if err == context.Canceled {
					return nil, nil
				}
				return nil, err
			}
		}
	})
	return err
}

func (s *runtimeState) runInteractiveSessionView(ctx context.Context, in io.Reader, out io.Writer, options liveViewOptions) error {
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

		printer := &liveSessionPrinter{
			rt:      rt,
			out:     out,
			options: options,
		}

		if options.PrintHeader {
			if _, err := fmt.Fprintln(out, formatInteractiveHeader(options, sendConversationID)); err != nil {
				return nil, err
			}
			if _, err := fmt.Fprintln(out, interactiveHelpText(options.AutoSteps)); err != nil {
				return nil, err
			}
		}

		if err := printer.printPending(ctx); err != nil {
			return nil, err
		}
		if !options.Follow {
			return nil, nil
		}

		lineCh := make(chan string)
		errCh := make(chan error, 1)
		go scanInput(in, lineCh, errCh)

		ticker := time.NewTicker(options.PollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return nil, nil
			case err := <-errCh:
				if err != nil {
					return nil, err
				}
				if err := printer.printPending(ctx); err != nil {
					return nil, err
				}
				return nil, nil
			case line, ok := <-lineCh:
				if !ok {
					if err := printer.printPending(ctx); err != nil {
						return nil, err
					}
					return nil, nil
				}
				if err := handleInteractiveLine(ctx, rt, out, printer, interactiveInput{
					line:               line,
					sessionID:          options.SessionID,
					sendConversationID: sendConversationID,
					agentsDir:          options.AgentsDir,
					autoSteps:          options.AutoSteps,
					orchestrationMode:  options.Orchestration,
					replyRoutingMode:   options.ReplyRouting,
				}); err != nil {
					if err == io.EOF {
						return nil, nil
					}
					return nil, err
				}
			case <-ticker.C:
				if err := printer.printPending(ctx); err != nil {
					return nil, err
				}
			}
		}
	})
	return err
}

func loadSessionAgentsDir(ctx context.Context, rt *runtimeadapter.Runtime, agentsRootDir string, sessionID domain.SessionID) (string, error) {
	if strings.TrimSpace(agentsRootDir) == "" {
		return "", fmt.Errorf("agents root directory must not be empty")
	}

	session, err := rt.GetSession(ctx, sessionID)
	if err != nil {
		return "", err
	}

	return resolveActorsDirFromRoot(agentsRootDir, session.ActorCatalog)
}

type liveSessionPrinter struct {
	rt      *runtimeadapter.Runtime
	out     io.Writer
	options liveViewOptions
	seen    int
}

func (p *liveSessionPrinter) printPending(ctx context.Context) error {
	snapshot, err := p.rt.InspectSession(ctx, p.options.SessionID)
	if err != nil {
		return err
	}

	if p.seen > len(snapshot.Stream) {
		p.seen = len(snapshot.Stream)
	}

	for _, entry := range snapshot.Stream[p.seen:] {
		line, ok := formatStreamEntry(entry, p.options.ConversationID)
		if !ok {
			continue
		}
		if _, err := fmt.Fprintln(p.out, line); err != nil {
			return err
		}
	}
	p.seen = len(snapshot.Stream)
	return nil
}

type interactiveInput struct {
	line               string
	sessionID          domain.SessionID
	sendConversationID domain.ConversationID
	agentsDir          string
	autoSteps          int
	orchestrationMode  application.OrchestrationMode
	replyRoutingMode   application.ReplyRoutingMode
}

func handleInteractiveLine(
	ctx context.Context,
	rt *runtimeadapter.Runtime,
	out io.Writer,
	printer *liveSessionPrinter,
	input interactiveInput,
) error {
	line := strings.TrimSpace(input.line)
	if line == "" {
		return nil
	}

	if strings.HasPrefix(line, "/") {
		return handleInteractiveCommand(ctx, rt, out, printer, input, line)
	}

	agents, _ := mustLoadAttachAgents(input.agentsDir)
	recipients := mentionedAgentIDs(line, agents)
	channel, policy := attachDispatchRouting(recipients)
	if _, err := rt.DispatchMessage(ctx, application.DispatchMessageCommand{
		SessionID:      input.sessionID,
		ConversationID: input.sendConversationID,
		Sender:         domain.UserSender("operator"),
		ToAgentIDs:     recipients,
		Channel:        channel,
		Kind:           domain.MessageKindUtterance,
		Body:           line,
		Policy:         policy,
	}); err != nil {
		return err
	}

	effectiveAutoSteps := effectiveAttachAutoSteps(input.autoSteps, recipients)
	if effectiveAutoSteps > 0 {
		if _, err := rt.AutoSession(ctx, application.AutoSessionCommand{
			SessionID:         input.sessionID,
			ConversationID:    input.sendConversationID,
			MaxSteps:          effectiveAutoSteps,
			OrchestrationMode: input.orchestrationMode,
			ReplyRoutingMode:  input.replyRoutingMode,
		}); err != nil {
			if _, writeErr := fmt.Fprintf(out, "[warning] auto-run failed: %v\n", err); writeErr != nil {
				return writeErr
			}
		}
	}

	return printer.printPending(ctx)
}

func handleInteractiveCommand(
	ctx context.Context,
	rt *runtimeadapter.Runtime,
	out io.Writer,
	printer *liveSessionPrinter,
	input interactiveInput,
	line string,
) error {
	fields := strings.Fields(line)
	switch fields[0] {
	case "/help":
		_, err := fmt.Fprintln(out, interactiveHelpText(input.autoSteps))
		return err
	case "/quit", "/exit":
		return io.EOF
	case "/step":
		if _, err := rt.StepSession(ctx, application.StepSessionCommand{
			SessionID:         input.sessionID,
			ConversationID:    input.sendConversationID,
			OrchestrationMode: input.orchestrationMode,
			ReplyRoutingMode:  input.replyRoutingMode,
		}); err != nil {
			return err
		}
		return printer.printPending(ctx)
	case "/auto":
		maxSteps := 3
		if len(fields) > 1 {
			value, err := strconv.Atoi(fields[1])
			if err != nil || value < 1 {
				return newCLIError("invalid_arguments", "usage: /auto [positive-step-count]")
			}
			maxSteps = value
		}
		if _, err := rt.AutoSession(ctx, application.AutoSessionCommand{
			SessionID:         input.sessionID,
			ConversationID:    input.sendConversationID,
			MaxSteps:          maxSteps,
			OrchestrationMode: input.orchestrationMode,
			ReplyRoutingMode:  input.replyRoutingMode,
		}); err != nil {
			return err
		}
		return printer.printPending(ctx)
	default:
		return newCLIError("invalid_arguments", fmt.Sprintf("unknown interactive command %q", fields[0]))
	}
}

func scanInput(in io.Reader, lineCh chan<- string, errCh chan<- error) {
	defer close(lineCh)

	scanner := bufio.NewScanner(in)
	for scanner.Scan() {
		lineCh <- scanner.Text()
	}

	errCh <- scanner.Err()
}

func formatLiveViewHeader(options liveViewOptions) string {
	header := fmt.Sprintf("attached to session %s", options.SessionID)
	if options.ConversationID != "" {
		header += fmt.Sprintf(" conversation %s", options.ConversationID)
	}
	if options.Follow {
		header += fmt.Sprintf(" follow=true poll=%s", options.PollInterval)
	} else {
		header += " follow=false"
	}
	return header
}

func formatInteractiveHeader(options liveViewOptions, sendConversationID domain.ConversationID) string {
	header := formatLiveViewHeader(options)
	header += fmt.Sprintf(" send_conversation=%s", sendConversationID)
	header += fmt.Sprintf(" auto_steps=%d orchestration=%s", options.AutoSteps, displayOrchestrationMode(options.Orchestration))
	return header
}

func interactiveHelpText(autoSteps int) string {
	if autoSteps > 0 {
		return fmt.Sprintf("commands: /help, /step, /auto [n], /quit. plain text sends a user message, @agent targets one or more agents directly, and auto-runs %d turn(s).", autoSteps)
	}
	return "commands: /help, /step, /auto [n], /quit. plain text sends a user message. Use @agent to target one or more agents directly."
}

func displayOrchestrationMode(mode application.OrchestrationMode) application.OrchestrationMode {
	if mode == "" {
		return application.OrchestrationModeDeterministic
	}
	return mode
}

func formatStreamEntry(entry runtimeadapter.StreamEntry, conversationID domain.ConversationID) (string, bool) {
	timestamp := entry.RecordedAt.UTC().Format(time.RFC3339)

	switch event := entry.Payload.(type) {
	case application.SessionCreatedEvent:
		return fmt.Sprintf("%s session created mode=%s status=%s", timestamp, event.Session.Mode, event.Session.Status), true
	case application.SessionUpdatedEvent:
		return fmt.Sprintf("%s session updated status=%s", timestamp, event.Session.Status), true
	case application.MessageDispatchedEvent:
		if conversationID != "" && event.Message.ConversationID != conversationID {
			return "", false
		}
		return fmt.Sprintf("%s %s", timestamp, formatMessageLine(event.Message, conversationID == "")), true
	case application.AgentTaskCreatedEvent:
		if conversationID != "" && event.Task.ConversationID != conversationID {
			return "", false
		}
		return fmt.Sprintf("%s %s", timestamp, formatTaskCreatedLine(event.Task, conversationID == "")), true
	case application.AgentTaskUpdatedEvent:
		if conversationID != "" && event.Task.ConversationID != conversationID {
			return "", false
		}
		return fmt.Sprintf("%s %s", timestamp, formatTaskUpdatedLine(event.Task, conversationID == "")), true
	case application.AgentHandoffCreatedEvent:
		if conversationID != "" && event.Handoff.ConversationID != conversationID {
			return "", false
		}
		return fmt.Sprintf("%s %s", timestamp, formatHandoffLine(event.Handoff, conversationID == "")), true
	default:
		return fmt.Sprintf("%s %s", timestamp, entry.Topic), true
	}
}

func formatMessageLine(message domain.Message, includeConversation bool) string {
	prefix := ""
	if includeConversation {
		prefix = fmt.Sprintf("[%s] ", message.ConversationID)
	}

	sender := message.Sender.ID
	if sender == "" {
		sender = string(message.Sender.Type)
	}

	var extras []string
	if len(message.ToAgentIDs) > 0 {
		recipients := make([]string, 0, len(message.ToAgentIDs))
		for _, id := range message.ToAgentIDs {
			recipients = append(recipients, string(id))
		}
		extras = append(extras, "to="+strings.Join(recipients, ","))
	}
	if message.ReplyTo != "" {
		extras = append(extras, "reply_to="+string(message.ReplyTo))
	}

	body := sanitizeLiveText(message.Body)
	if len(extras) == 0 {
		return fmt.Sprintf("%s%s: %s", prefix, sender, body)
	}

	return fmt.Sprintf("%s%s (%s): %s", prefix, sender, strings.Join(extras, " "), body)
}

func formatTaskCreatedLine(task application.SandboxTask, includeConversation bool) string {
	prefix := ""
	if includeConversation {
		prefix = fmt.Sprintf("[%s] ", task.ConversationID)
	}
	return fmt.Sprintf("%ssandbox task %s created runtime=%s instruction=%q", prefix, task.ID, task.RuntimeName, sanitizeLiveText(task.Instruction))
}

func formatTaskUpdatedLine(task application.SandboxTask, includeConversation bool) string {
	prefix := ""
	if includeConversation {
		prefix = fmt.Sprintf("[%s] ", task.ConversationID)
	}
	line := fmt.Sprintf("%ssandbox task %s %s", prefix, task.ID, task.Status)
	if summary := sanitizeLiveText(task.ResultSummary); summary != "" {
		line += fmt.Sprintf(" summary=%q", summary)
	}
	if errText := sanitizeLiveText(task.ErrorMessage); errText != "" {
		line += fmt.Sprintf(" error=%q", errText)
	}
	return line
}

func formatHandoffLine(handoff application.AgentHandoff, includeConversation bool) string {
	prefix := ""
	if includeConversation {
		prefix = fmt.Sprintf("[%s] ", handoff.ConversationID)
	}
	target := handoff.ToAgentID
	if target == "" {
		target = domain.AgentID(handoff.ToProviderClass)
	}
	return fmt.Sprintf("%shandoff %s -> %s task=%s reason=%q", prefix, handoff.FromAgentID, target, handoff.TaskID, sanitizeLiveText(handoff.Reason))
}

func sanitizeLiveText(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\t", " ")
	return strings.TrimSpace(value)
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
