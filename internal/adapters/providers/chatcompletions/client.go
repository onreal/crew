package chatcompletions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"crew/internal/adapters/providers/structuredgeneration"
	"crew/internal/application"
	"crew/internal/domain"
)

type Config struct {
	ProviderName string
	BaseURL      string
	APIKey       string
	Timeout      time.Duration
	Temperature  float64
	HTTPClient   *http.Client
}

type Client struct {
	providerName string
	baseURL      string
	apiKey       string
	timeout      time.Duration
	temperature  float64
	httpClient   *http.Client
}

func New(cfg Config) (*Client, error) {
	providerName := strings.TrimSpace(cfg.ProviderName)
	if providerName == "" {
		providerName = "openai"
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL(providerName)
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("%s api key must not be empty", providerName)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}

	return &Client{
		providerName: providerName,
		baseURL:      baseURL,
		apiKey:       cfg.APIKey,
		timeout:      timeout,
		temperature:  cfg.Temperature,
		httpClient:   httpClient,
	}, nil
}

func (c *Client) Generate(ctx context.Context, request application.GenerationRequest) (application.GenerationResult, error) {
	if len(request.Messages) == 0 {
		return application.GenerationResult{}, fmt.Errorf("%s generation requires at least one message", c.providerName)
	}
	if strings.TrimSpace(request.Agent.Model) == "" {
		return application.GenerationResult{}, fmt.Errorf("%s generation requires agent model", c.providerName)
	}

	payload := chatCompletionRequest{
		Model:       requestModel(request.Agent),
		Temperature: c.temperature,
		Messages: []chatMessage{
			{Role: "system", Content: structuredgeneration.SystemInstruction(request.Agent)},
			{Role: "user", Content: structuredgeneration.TranscriptPrompt(request)},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return application.GenerationResult{}, fmt.Errorf("marshal %s request: %w", c.providerName, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return application.GenerationResult{}, fmt.Errorf("build %s request: %w", c.providerName, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return application.GenerationResult{}, fmt.Errorf("send %s request: %w", c.providerName, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return application.GenerationResult{}, fmt.Errorf("read %s response: %w", c.providerName, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return application.GenerationResult{}, fmt.Errorf("%s request failed: status=%d body=%s", c.providerName, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var decoded chatCompletionResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return application.GenerationResult{}, fmt.Errorf("decode %s response: %w", c.providerName, err)
	}
	if len(decoded.Choices) == 0 {
		return application.GenerationResult{}, fmt.Errorf("%s response contained no choices", c.providerName)
	}

	content := strings.TrimSpace(decoded.Choices[0].Message.Content)
	if content == "" {
		return application.GenerationResult{}, nil
	}

	result := structuredgeneration.ParseResult(content)
	result.Metadata = map[string]any{
		"generated_by": c.providerName + "_llm",
		"provider":     c.providerName,
		"model":        decoded.Model,
	}
	return result, nil
}

func defaultBaseURL(providerName string) string {
	switch providerName {
	case "gemini":
		return "https://generativelanguage.googleapis.com/v1beta/openai"
	case "grok":
		return "https://api.x.ai/v1"
	default:
		return "https://api.openai.com/v1"
	}
}

func requestModel(agent domain.Agent) string {
	return strings.TrimSpace(agent.Model)
}

type chatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Model   string                 `json:"model"`
	Choices []chatCompletionChoice `json:"choices"`
}

type chatCompletionChoice struct {
	Message chatMessage `json:"message"`
}

type generationEnvelope struct {
	MessageBody    string                 `json:"message_body"`
	SandboxRequest *sandboxRequestPayload `json:"sandbox_request"`
}

type sandboxRequestPayload struct {
	Instruction       string `json:"instruction"`
	PermissionProfile string `json:"permission_profile"`
}
