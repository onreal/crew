package main

import "strings"

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
		name := strings.ToLower(strings.TrimPrefix(command.Command, "/"))
		if query != "" && !strings.HasPrefix(name, query) {
			continue
		}
		suggestions = append(suggestions, attachInputSuggestion{
			Label: command.Command, InsertValue: command.InsertValue, Description: command.Description,
		})
	}
	if len(suggestions) == 0 {
		return attachInputAssist{}
	}
	return attachInputAssist{
		Kind: attachInputAssistCommand, Start: 0, End: commandEnd, Suggestions: suggestions,
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
			Label: label, InsertValue: "@" + string(agent.ID), Description: agent.Name,
		})
	}
	if len(suggestions) == 0 {
		return attachInputAssist{}
	}
	return attachInputAssist{
		Kind: attachInputAssistMention, Start: start, End: end, Suggestions: suggestions,
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
	return strings.TrimSpace(string(runes[start:end]))
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
