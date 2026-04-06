package application

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"

	"crew/internal/domain"
)

type VectorService struct {
	sessions SessionRepository
	messages MessageRepository
	index    VectorIndex
	admin    VectorAdmin
	embedder Embedder
}

func NewVectorService(
	sessions SessionRepository,
	messages MessageRepository,
	index VectorIndex,
	admin VectorAdmin,
	embedder Embedder,
) *VectorService {
	return &VectorService{
		sessions: sessions,
		messages: messages,
		index:    index,
		admin:    admin,
		embedder: embedder,
	}
}

func (s *VectorService) Status(ctx context.Context, query VectorStatusQuery) (VectorIndexState, VectorIndexStatus, error) {
	if err := query.Validate(); err != nil {
		return VectorIndexState{}, "", err
	}
	if err := s.ensureSessionExists(ctx, query.SessionID); err != nil {
		return VectorIndexState{}, "", err
	}
	if s.index == nil || s.admin == nil {
		return VectorIndexState{
			IndexName: defaultVectorIndexName(query.SessionID),
			Status:    VectorIndexStateStatusDisabled,
		}, VectorIndexStatusDisabled, nil
	}

	backendStatus, err := s.index.Status(ctx)
	if err != nil {
		return VectorIndexState{}, "", err
	}

	if query.SessionID == "" {
		state, err := s.admin.State(ctx)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return VectorIndexState{
					IndexName: defaultVectorIndexName(""),
					Provider:  missingStateProvider(backendStatus),
					Status:    deriveMissingStateStatus(backendStatus),
				}, backendStatus, nil
			}
			return VectorIndexState{}, "", err
		}
		return state, backendStatus, nil
	}

	state, err := s.admin.StateForSession(ctx, query.SessionID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return VectorIndexState{
				IndexName: defaultVectorIndexName(query.SessionID),
				Provider:  missingStateProvider(backendStatus),
				Status:    deriveMissingStateStatus(backendStatus),
			}, backendStatus, nil
		}
		return VectorIndexState{}, "", err
	}

	return state, backendStatus, nil
}

func (s *VectorService) Rebuild(ctx context.Context, cmd VectorRebuildCommand) (VectorRebuildStats, VectorIndexState, VectorIndexStatus, error) {
	if err := cmd.Validate(); err != nil {
		return VectorRebuildStats{}, VectorIndexState{}, "", err
	}
	if err := s.ensureSessionExists(ctx, cmd.SessionID); err != nil {
		return VectorRebuildStats{}, VectorIndexState{}, "", err
	}
	if s.index == nil || s.admin == nil || s.embedder == nil {
		return VectorRebuildStats{}, VectorIndexState{
			IndexName: defaultVectorIndexName(cmd.SessionID),
			Status:    VectorIndexStateStatusDisabled,
		}, VectorIndexStatusDisabled, ErrDisabled
	}

	backendStatus, err := s.index.Status(ctx)
	if err != nil {
		return VectorRebuildStats{}, VectorIndexState{}, "", err
	}

	stats, err := s.admin.RebuildFromCanonicalMessages(ctx, s.embedder, VectorRebuildOptions{
		SessionID: cmd.SessionID,
		Force:     cmd.Force,
	})
	if err != nil {
		return VectorRebuildStats{}, VectorIndexState{}, "", err
	}

	state, backendStatus, err := s.Status(ctx, VectorStatusQuery{SessionID: cmd.SessionID})
	if err != nil {
		return VectorRebuildStats{}, VectorIndexState{}, "", err
	}

	return stats, state, backendStatus, nil
}

func (s *VectorService) RecallSessionMessages(ctx context.Context, query RecallSessionMessagesQuery) (VectorRecallResponse, error) {
	if err := query.Validate(); err != nil {
		return VectorRecallResponse{}, err
	}

	if _, err := s.sessions.GetByID(ctx, query.SessionID); err != nil {
		return VectorRecallResponse{}, err
	}

	messages, err := s.messages.ListBySessionID(ctx, query.SessionID)
	if err != nil {
		return VectorRecallResponse{}, err
	}

	statusResult, err := s.statusForRecall(ctx, query.SessionID)
	if err != nil {
		return VectorRecallResponse{}, err
	}

	response := VectorRecallResponse{
		SessionID:     query.SessionID,
		QueryText:     query.QueryText,
		BackendStatus: statusResult.backendStatus,
		IndexState:    statusResult.state,
	}

	if s.index == nil || s.embedder == nil || s.admin == nil {
		response.FallbackUsed = true
		response.FallbackReason = "vector_unavailable"
		response.Results = lexicalFallbackResults(messages, query.QueryText, query.Limit)
		return response, nil
	}

	if !isStateQueryable(statusResult.backendStatus, statusResult.state.Status) {
		response.FallbackUsed = true
		if statusResult.backendStatus == VectorIndexStatusDisabled {
			response.FallbackReason = string(VectorIndexStateStatusDisabled)
		} else {
			response.FallbackReason = string(statusResult.state.Status)
		}
		response.Results = lexicalFallbackResults(messages, query.QueryText, query.Limit)
		return response, nil
	}

	embedding, err := s.embedder.EmbedText(ctx, query.QueryText)
	if err != nil {
		response.FallbackUsed = true
		response.FallbackReason = "embed_failed"
		response.Results = lexicalFallbackResults(messages, query.QueryText, query.Limit)
		return response, nil
	}

	results, err := s.index.SearchMessages(ctx, VectorSearchQuery{
		SessionID: query.SessionID,
		Embedding: embedding,
		Limit:     query.Limit,
	})
	if err != nil {
		response.FallbackUsed = true
		response.FallbackReason = "search_failed"
		response.Results = lexicalFallbackResults(messages, query.QueryText, query.Limit)
		return response, nil
	}

	messageByID := make(map[domain.MessageID]domain.Message, len(messages))
	for _, message := range messages {
		messageByID[message.ID] = message
	}

	response.Results = make([]VectorRecallResult, 0, len(results))
	for _, result := range results {
		message, ok := messageByID[result.MessageID]
		if !ok {
			continue
		}
		distance := result.Distance
		response.Results = append(response.Results, VectorRecallResult{
			Message:  message,
			Distance: &distance,
			Strategy: "vector",
		})
	}

	return response, nil
}

func (s *VectorService) statusForRecall(ctx context.Context, sessionID domain.SessionID) (vectorStatusResult, error) {
	state, backendStatus, err := s.Status(ctx, VectorStatusQuery{SessionID: sessionID})
	if err != nil {
		return vectorStatusResult{}, err
	}
	return vectorStatusResult{
		state:         state,
		backendStatus: backendStatus,
	}, nil
}

type vectorStatusResult struct {
	state         VectorIndexState
	backendStatus VectorIndexStatus
}

func deriveMissingStateStatus(backendStatus VectorIndexStatus) VectorIndexStateStatus {
	if backendStatus == VectorIndexStatusDisabled {
		return VectorIndexStateStatusDisabled
	}
	return VectorIndexStateStatusStale
}

func missingStateProvider(backendStatus VectorIndexStatus) string {
	if backendStatus == VectorIndexStatusDisabled {
		return "disabled"
	}
	return ""
}

func isStateQueryable(backendStatus VectorIndexStatus, stateStatus VectorIndexStateStatus) bool {
	return backendStatus == VectorIndexStatusReady && stateStatus == VectorIndexStateStatusReady
}

func lexicalFallbackResults(messages []domain.Message, queryText string, limit int) []VectorRecallResult {
	type scored struct {
		message domain.Message
		score   int
	}

	normalizedQuery := strings.ToLower(queryText)
	tokens := uniqueQueryTokens(normalizedQuery)
	scoredMessages := make([]scored, 0, len(messages))
	for _, message := range messages {
		body := strings.ToLower(message.Body)
		score := 0
		if strings.Contains(body, normalizedQuery) {
			score += 100
		}
		for _, token := range tokens {
			if strings.Contains(body, token) {
				score++
			}
		}
		if score == 0 {
			continue
		}
		scoredMessages = append(scoredMessages, scored{message: message, score: score})
	}

	sort.SliceStable(scoredMessages, func(i, j int) bool {
		if scoredMessages[i].score != scoredMessages[j].score {
			return scoredMessages[i].score > scoredMessages[j].score
		}
		if !scoredMessages[i].message.Timestamp.Equal(scoredMessages[j].message.Timestamp) {
			return scoredMessages[i].message.Timestamp.After(scoredMessages[j].message.Timestamp)
		}
		return scoredMessages[i].message.ID < scoredMessages[j].message.ID
	})

	if len(scoredMessages) == 0 {
		recent := slices.Clone(messages)
		sort.SliceStable(recent, func(i, j int) bool {
			if !recent[i].Timestamp.Equal(recent[j].Timestamp) {
				return recent[i].Timestamp.After(recent[j].Timestamp)
			}
			return recent[i].ID < recent[j].ID
		})
		if len(recent) > limit {
			recent = recent[:limit]
		}

		fallback := make([]VectorRecallResult, 0, len(recent))
		for _, message := range recent {
			fallback = append(fallback, VectorRecallResult{
				Message:  message,
				Strategy: "recent_fallback",
			})
		}
		return fallback
	}

	if len(scoredMessages) > limit {
		scoredMessages = scoredMessages[:limit]
	}

	fallback := make([]VectorRecallResult, 0, len(scoredMessages))
	for _, item := range scoredMessages {
		fallback = append(fallback, VectorRecallResult{
			Message:  item.message,
			Strategy: "lexical_fallback",
		})
	}

	return fallback
}

func uniqueQueryTokens(queryText string) []string {
	raw := strings.FieldsFunc(queryText, func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\t' || r == ',' || r == '.' || r == ':' || r == ';' || r == '!' || r == '?' || r == '"' || r == '\'' || r == '(' || r == ')' || r == '[' || r == ']'
	})

	seen := make(map[string]struct{}, len(raw))
	tokens := make([]string, 0, len(raw))
	for _, token := range raw {
		token = strings.TrimSpace(token)
		if len(token) < 2 {
			continue
		}
		if _, exists := seen[token]; exists {
			continue
		}
		seen[token] = struct{}{}
		tokens = append(tokens, token)
	}
	return tokens
}

func defaultVectorIndexName(sessionID domain.SessionID) string {
	if sessionID == "" {
		return "messages"
	}
	return "messages/session/" + string(sessionID)
}

func (s *VectorService) ensureSessionExists(ctx context.Context, sessionID domain.SessionID) error {
	if sessionID == "" {
		return nil
	}
	if s.sessions == nil {
		return ErrDisabled
	}
	_, err := s.sessions.GetByID(ctx, sessionID)
	return err
}
