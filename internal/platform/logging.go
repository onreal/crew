package platform

import (
	"fmt"
	"log/slog"
	"os"
)

func NewLogger(levelText string) (*slog.Logger, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(levelText)); err != nil {
		return nil, fmt.Errorf("parse log level %q: %w", levelText, err)
	}

	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})), nil
}
