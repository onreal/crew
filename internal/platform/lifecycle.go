package platform

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func NewSignalContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}
