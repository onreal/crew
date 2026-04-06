package chatcompletions

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"crew/internal/application"
	"crew/internal/domain"
)

func TestClientGenerateUsesChatCompletionsProtocol(t *testing.T) {
	var captured chatCompletionRequest

	client, err := New(Config{
		ProviderName: "openai",
		BaseURL:      "http://provider.test/v1",
		APIKey:       "secret-key",
		Timeout:      5 * time.Second,
		Temperature:  0.1,
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/v1/chat/completions" {
					t.Fatalf("unexpected path %q", r.URL.Path)
				}
				if auth := r.Header.Get("Authorization"); auth != "Bearer secret-key" {
					t.Fatalf("unexpected auth header %q", auth)
				}
				if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
					t.Fatalf("Decode() error = %v", err)
				}

				return jsonResponse(http.StatusOK, map[string]any{
					"model": "gpt-test",
					"choices": []map[string]any{
						{
							"message": map[string]any{
								"content": `{"message_body":"Provider reply","sandbox_request":null}`,
							},
						},
					},
				}), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := client.Generate(context.Background(), application.GenerationRequest{
		Agent: mustAgent(),
		Messages: []domain.Message{
			mustMessage("message-1", domain.UserSender("operator"), "review the runtime recovery path"),
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if result.MessageBody != "Provider reply" {
		t.Fatalf("expected provider reply, got %q", result.MessageBody)
	}
	if captured.Model != "gpt-test" {
		t.Fatalf("expected model gpt-test, got %q", captured.Model)
	}
	if len(captured.Messages) != 2 {
		t.Fatalf("expected 2 prompt messages, got %d", len(captured.Messages))
	}
	if captured.Messages[0].Role != "system" {
		t.Fatalf("expected system message, got %+v", captured.Messages[0])
	}
	if !strings.Contains(captured.Messages[1].Content, "review the runtime recovery path") {
		t.Fatalf("expected transcript prompt to contain session text, got %q", captured.Messages[1].Content)
	}
	if !strings.Contains(captured.Messages[0].Content, "\"message_body\"") {
		t.Fatalf("expected structured output instructions in system prompt, got %q", captured.Messages[0].Content)
	}
}

func TestClientGenerateUsesAgentModel(t *testing.T) {
	var captured chatCompletionRequest

	client, err := New(Config{
		ProviderName: "openai",
		BaseURL:      "http://provider.test/v1",
		APIKey:       "secret-key",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
					t.Fatalf("Decode() error = %v", err)
				}
				return jsonResponse(http.StatusOK, map[string]any{
					"model": "agent-model",
					"choices": []map[string]any{
						{
							"message": map[string]any{
								"content": `{"message_body":"Provider reply","sandbox_request":null}`,
							},
						},
					},
				}), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	agent, err := domain.NewAgent(domain.Agent{
		ID:           "writer",
		Name:         "Writer",
		Role:         "writer",
		SystemPrompt: "Draft the next message.",
		Provider:     "openai",
		Model:        "agent-model",
	})
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}

	if _, err := client.Generate(context.Background(), application.GenerationRequest{
		Agent:    agent,
		Messages: []domain.Message{mustMessage("message-1", domain.UserSender("operator"), "hello")},
	}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if captured.Model != "agent-model" {
		t.Fatalf("expected agent model override, got %q", captured.Model)
	}
}

func TestClientGenerateRejectsEmptyChoices(t *testing.T) {
	client, err := New(Config{
		ProviderName: "openai",
		BaseURL:      "http://provider.test/v1",
		APIKey:       "secret-key",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, map[string]any{
					"model":   "gpt-test",
					"choices": []map[string]any{},
				}), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = client.Generate(context.Background(), application.GenerationRequest{
		Agent:    mustAgent(),
		Messages: []domain.Message{mustMessage("message-1", domain.UserSender("operator"), "hello")},
	})
	if err == nil {
		t.Fatal("expected Generate() to reject empty choices")
	}
}

func TestClientGenerateParsesSandboxRequest(t *testing.T) {
	client, err := New(Config{
		ProviderName: "openai",
		BaseURL:      "http://provider.test/v1",
		APIKey:       "secret-key",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, map[string]any{
					"model": "gpt-test",
					"choices": []map[string]any{
						{
							"message": map[string]any{
								"content": `{"message_body":"Delegating sandbox work","sandbox_request":{"instruction":"update the README","permission_profile":"patch"}}`,
							},
						},
					},
				}), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := client.Generate(context.Background(), application.GenerationRequest{
		Agent:    mustSandboxAgent(),
		Messages: []domain.Message{mustMessage("message-1", domain.UserSender("operator"), "delegate the patch work")},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if result.SandboxRequest == nil {
		t.Fatal("expected parsed sandbox request")
	}
	if result.SandboxRequest.Instruction != "update the README" {
		t.Fatalf("unexpected sandbox instruction %q", result.SandboxRequest.Instruction)
	}
	if result.SandboxRequest.PermissionProfile != application.SandboxPermissionPatch {
		t.Fatalf("unexpected sandbox permission profile %q", result.SandboxRequest.PermissionProfile)
	}
}

func mustAgent() domain.Agent {
	agent, err := domain.NewAgent(domain.Agent{
		ID:           "planner",
		Name:         "Planner",
		Role:         "planner",
		SystemPrompt: "Plan the next step.",
		Provider:     "openai",
		Model:        "gpt-test",
	})
	if err != nil {
		panic(err)
	}
	return agent
}

func mustSandboxAgent() domain.Agent {
	agent, err := domain.NewAgent(domain.Agent{
		ID:           "planner",
		Name:         "Planner",
		Role:         "planner",
		SystemPrompt: "Plan the next step.",
		Provider:     "openai",
		Model:        "gpt-test",
		Policies: domain.AgentPolicy{
			AllowBroadcast:         true,
			AllowToolCalls:         true,
			AllowSandboxDelegation: true,
			AllowedSandboxRuntimes: []string{"codex"},
			MaxConsecutiveTurns:    2,
			MaxToolCallsPerTurn:    1,
		},
	})
	if err != nil {
		panic(err)
	}
	return agent
}

func mustMessage(id domain.MessageID, sender domain.MessageSender, body string) domain.Message {
	message, err := domain.NewMessage(domain.Message{
		ID:             id,
		SessionID:      "session-1",
		ConversationID: "conversation-1",
		Sender:         sender,
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           body,
		Timestamp:      time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		panic(err)
	}
	return message
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonResponse(status int, payload map[string]any) *http.Response {
	body, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}

	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(body))),
	}
}
