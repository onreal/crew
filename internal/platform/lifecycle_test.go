package platform

import (
	"context"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func TestNewSignalContextCancelsOnSIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal semantics differ on windows")
	}

	ctx, cancel := NewSignalContext(context.Background())
	defer cancel()

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("Kill() error = %v", err)
	}

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("expected context cancellation on SIGTERM")
	}
}
