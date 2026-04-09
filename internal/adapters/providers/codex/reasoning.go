package codex

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
)

type ReasoningEvent struct {
	AgentID string
	Text    string
}

type reasoningReporterKey struct{}

func WithReasoningReporter(ctx context.Context, report func(ReasoningEvent)) context.Context {
	if report == nil {
		return ctx
	}
	return context.WithValue(ctx, reasoningReporterKey{}, report)
}

func reasoningReporterFromContext(ctx context.Context) func(ReasoningEvent) {
	report, _ := ctx.Value(reasoningReporterKey{}).(func(ReasoningEvent))
	return report
}

type jsonlReasoningSink struct {
	agentID string
	report  func(ReasoningEvent)

	mu   sync.Mutex
	buf  string
	last string
}

func newJSONLReasoningSink(agentID string, report func(ReasoningEvent)) *jsonlReasoningSink {
	if report == nil {
		return nil
	}
	return &jsonlReasoningSink{agentID: agentID, report: report}
}

func (s *jsonlReasoningSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.buf += string(p)
	for {
		idx := strings.IndexByte(s.buf, '\n')
		if idx < 0 {
			break
		}
		s.processLine(s.buf[:idx])
		s.buf = s.buf[idx+1:]
	}
	return len(p), nil
}

func (s *jsonlReasoningSink) processLine(line string) {
	text := extractProgressText(strings.TrimSpace(line))
	if text == "" || text == s.last {
		return
	}
	s.last = text
	s.report(ReasoningEvent{AgentID: s.agentID, Text: text})
}

func extractProgressText(line string) string {
	if line == "" {
		return ""
	}

	var payload any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return ""
	}
	if isTerminalProgressPayload(payload) {
		return ""
	}
	text := strings.TrimSpace(findProgressText(payload, ""))
	if text != "" {
		return text
	}
	return strings.TrimSpace(fallbackProgressLabel(payload))
}

func findProgressText(value any, parentKey string) string {
	switch typed := value.(type) {
	case map[string]any:
		contextHint := strings.Contains(parentKey, "reason")
		for key, child := range typed {
			lowerKey := strings.ToLower(strings.TrimSpace(key))
			if strings.Contains(lowerKey, "reason") ||
				lowerKey == "summary" ||
				lowerKey == "message" ||
				lowerKey == "delta" ||
				lowerKey == "content" ||
				lowerKey == "text" ||
				strings.Contains(lowerKey, "progress") {
				contextHint = true
			}
			if lowerKey == "type" || lowerKey == "event" || lowerKey == "kind" || lowerKey == "channel" || lowerKey == "role" {
				valueText := strings.ToLower(stringValue(child))
				if strings.Contains(valueText, "reason") || strings.Contains(valueText, "progress") || strings.Contains(valueText, "thinking") {
					contextHint = true
				}
			}
		}
		if contextHint {
			for _, key := range []string{"text", "summary", "content", "message", "delta", "output"} {
				if text := strings.TrimSpace(stringValue(typed[key])); text != "" {
					return text
				}
			}
		}
		for key, child := range typed {
			if text := findProgressText(child, strings.ToLower(strings.TrimSpace(key))); text != "" {
				return text
			}
		}
	case []any:
		for _, child := range typed {
			if text := findProgressText(child, parentKey); text != "" {
				return text
			}
		}
	case string:
		if strings.Contains(parentKey, "reason") || strings.Contains(parentKey, "progress") {
			return typed
		}
	}
	return ""
}

func fallbackProgressLabel(value any) string {
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"type", "event", "kind", "status"} {
		label := strings.TrimSpace(stringValue(object[key]))
		if label == "" {
			continue
		}
		lower := strings.ToLower(label)
		if strings.Contains(lower, "reason") || strings.Contains(lower, "progress") || strings.Contains(lower, "thinking") {
			return label
		}
	}
	return ""
}

func isTerminalProgressPayload(value any) bool {
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	if body := strings.TrimSpace(stringValue(object["message_body"])); body != "" {
		return true
	}
	for _, key := range []string{"type", "event", "kind", "status"} {
		label := strings.ToLower(strings.TrimSpace(stringValue(object[key])))
		if label == "" {
			continue
		}
		if strings.Contains(label, "completed") || strings.Contains(label, "complete") || strings.Contains(label, "finished") || strings.Contains(label, "final") || label == "done" {
			return true
		}
	}
	return false
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		for _, key := range []string{"text", "summary", "content"} {
			if text := strings.TrimSpace(stringValue(typed[key])); text != "" {
				return text
			}
		}
	case []any:
		parts := make([]string, 0, len(typed))
		for _, child := range typed {
			if text := strings.TrimSpace(stringValue(child)); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}
