package grok

import (
	"net/http"
	"time"

	"crew/internal/adapters/providers/chatcompletions"
)

type Config struct {
	BaseURL     string
	APIKey      string
	Timeout     time.Duration
	Temperature float64
	HTTPClient  *http.Client
}

func New(cfg Config) (*chatcompletions.Client, error) {
	return chatcompletions.New(chatcompletions.Config{
		ProviderName: "grok",
		BaseURL:      cfg.BaseURL,
		APIKey:       cfg.APIKey,
		Timeout:      cfg.Timeout,
		Temperature:  cfg.Temperature,
		HTTPClient:   cfg.HTTPClient,
	})
}
