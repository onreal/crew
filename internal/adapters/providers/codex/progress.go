package codex

import (
	"encoding/json"
	"strings"
	"sync"

	"crew/internal/application"
	"crew/internal/domain"
)

type jsonlProgressSink struct {
	agentID domain.AgentID
	report  func(application.TransientProgressEvent)

	mu      sync.Mutex
	buf     string
	last    string
	streams map[string]string
}

func newJSONLProgressSink(agentID domain.AgentID, report func(application.TransientProgressEvent)) *jsonlProgressSink {
	if report == nil {
		return nil
	}
	return &jsonlProgressSink{
		agentID: agentID,
		report:  report,
		streams: make(map[string]string),
	}
}

func (s *jsonlProgressSink) Write(p []byte) (int, error) {
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

func (s *jsonlProgressSink) processLine(line string) {
	event, ok := extractProgressEvent(s.agentID, strings.TrimSpace(line))
	if !ok {
		return
	}
	event = s.coalesce(event)
	if strings.TrimSpace(event.Text) == "" {
		return
	}
	dedupe := event.Kind + "|" + event.RawType + "|" + event.Text
	if dedupe == "" || dedupe == s.last {
		return
	}
	s.last = dedupe
	s.report(event)
}

func (s *jsonlProgressSink) coalesce(event application.TransientProgressEvent) application.TransientProgressEvent {
	streamKey := progressStreamKey(event)
	if streamKey == "" {
		return event
	}
	if isDeltaProgressType(event.RawType) {
		s.streams[streamKey] += event.Text
		event.Text = strings.TrimSpace(s.streams[streamKey])
		return event
	}
	s.streams[streamKey] = strings.TrimSpace(event.Text)
	return event
}

func extractProgressEvent(agentID domain.AgentID, line string) (application.TransientProgressEvent, bool) {
	if line == "" {
		return application.TransientProgressEvent{}, false
	}

	var payload any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return application.TransientProgressEvent{}, false
	}

	rawType, kind := progressLabels(payload)
	if strings.Contains(strings.ToLower(rawType), "raw_content") {
		return application.TransientProgressEvent{}, false
	}
	if isTerminalProgressPayload(payload) && kind == "" {
		return application.TransientProgressEvent{}, false
	}
	text := findProgressText(payload, "")
	if kind == "" && strings.TrimSpace(text) == "" {
		return application.TransientProgressEvent{}, false
	}
	if strings.TrimSpace(text) == "" {
		text = fallbackProgressLabel(payload)
	}
	if !isDeltaProgressType(rawType) {
		text = strings.TrimSpace(text)
	}
	if strings.TrimSpace(text) == "" {
		return application.TransientProgressEvent{}, false
	}

	return application.TransientProgressEvent{
		Provider: "codex",
		AgentID:  agentID,
		Kind:     kind,
		RawType:  rawType,
		Text:     text,
	}, true
}

func progressStreamKey(event application.TransientProgressEvent) string {
	rawType := strings.ToLower(strings.TrimSpace(event.RawType))
	switch {
	case strings.Contains(rawType, "reason"):
		return "reasoning"
	case strings.Contains(rawType, "think"):
		return "thinking"
	case strings.Contains(rawType, "progress"):
		return "progress"
	case strings.TrimSpace(event.Kind) != "":
		return strings.TrimSpace(event.Kind)
	default:
		return ""
	}
}

func isDeltaProgressType(rawType string) bool {
	rawType = strings.ToLower(strings.TrimSpace(rawType))
	return strings.Contains(rawType, ".delta") || strings.HasSuffix(rawType, "_delta")
}

func progressLabels(value any) (string, string) {
	object, ok := value.(map[string]any)
	if !ok {
		return "", ""
	}

	rawType := strings.TrimSpace(firstStringValue(
		object["type"],
		object["event"],
		object["kind"],
		object["status"],
	))
	itemType := strings.TrimSpace(progressItemType(object))
	combined := strings.ToLower(strings.TrimSpace(rawType + " " + itemType))

	switch {
	case strings.Contains(combined, "reason"):
		return rawType, "reasoning"
	case strings.Contains(combined, "think"):
		return rawType, "thinking"
	case strings.Contains(combined, "progress"):
		return rawType, "progress"
	default:
		return rawType, ""
	}
}

func progressItemType(object map[string]any) string {
	for _, key := range []string{"item_type", "output_type"} {
		if value := strings.TrimSpace(stringValue(object[key])); value != "" {
			return value
		}
	}
	item, _ := object["item"].(map[string]any)
	if item == nil {
		return ""
	}
	for _, key := range []string{"type", "item_type", "kind", "status"} {
		if value := strings.TrimSpace(stringValue(item[key])); value != "" {
			return value
		}
	}
	return ""
}

func findProgressText(value any, parentKey string) string {
	switch typed := value.(type) {
	case map[string]any:
		contextHint := strings.Contains(parentKey, "reason") || strings.Contains(parentKey, "think") || strings.Contains(parentKey, "progress")
		for key, child := range typed {
			lowerKey := strings.ToLower(strings.TrimSpace(key))
			if strings.Contains(lowerKey, "reason") ||
				strings.Contains(lowerKey, "think") ||
				lowerKey == "summary_text" ||
				lowerKey == "reasoning_text" ||
				lowerKey == "summary" ||
				lowerKey == "message" ||
				lowerKey == "delta" ||
				lowerKey == "content" ||
				lowerKey == "text" ||
				lowerKey == "reasoning_content" ||
				strings.Contains(lowerKey, "progress") {
				contextHint = true
			}
			if lowerKey == "type" || lowerKey == "event" || lowerKey == "kind" || lowerKey == "channel" || lowerKey == "role" || lowerKey == "item_type" {
				valueText := strings.ToLower(stringValue(child))
				if strings.Contains(valueText, "reason") || strings.Contains(valueText, "progress") || strings.Contains(valueText, "thinking") {
					contextHint = true
				}
			}
		}
		if contextHint {
			for _, key := range []string{"text", "summary_text", "reasoning_text", "summary", "content", "message", "delta", "output", "output_text", "reasoning_content"} {
				text := stringValue(typed[key])
				if key != "delta" {
					text = strings.TrimSpace(text)
				}
				if strings.TrimSpace(text) != "" {
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
		if strings.Contains(parentKey, "reason") || strings.Contains(parentKey, "progress") || strings.Contains(parentKey, "think") {
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

	itemType := strings.ToLower(strings.TrimSpace(progressItemType(object)))
	if itemType != "" && (strings.Contains(itemType, "reason") || strings.Contains(itemType, "think") || strings.Contains(itemType, "progress")) {
		return itemType
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
		for _, key := range []string{"text", "summary_text", "reasoning_text", "summary", "content", "message", "output_text", "reasoning_content"} {
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

func firstStringValue(values ...any) string {
	for _, value := range values {
		if text := strings.TrimSpace(stringValue(value)); text != "" {
			return text
		}
	}
	return ""
}
