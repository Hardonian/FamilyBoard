package watchdog

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestWatchdog_NoSocket(t *testing.T) {
	_ = os.Unsetenv("NOTIFY_SOCKET")
	_ = os.Unsetenv("WATCHDOG_USEC")

	if err := NotifyReady(); err != nil {
		t.Errorf("expected nil error when NOTIFY_SOCKET is unset, got: %v", err)
	}
	if err := NotifyWatchdog(); err != nil {
		t.Errorf("expected nil error when NOTIFY_SOCKET is unset, got: %v", err)
	}
	if err := NotifyStopping(); err != nil {
		t.Errorf("expected nil error when NOTIFY_SOCKET is unset, got: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	// Should not panic or hang
	StartWatchdog(ctx)
}
