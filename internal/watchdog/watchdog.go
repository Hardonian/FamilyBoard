package watchdog

import (
	"context"
	"log/slog"
	"net"
	"os"
	"strconv"
	"time"
)

// Notify sends a state message to systemd via NOTIFY_SOCKET if configured.
func Notify(state string) error {
	socketAddr := os.Getenv("NOTIFY_SOCKET")
	if socketAddr == "" {
		return nil
	}

	conn, err := net.Dial("unixgram", socketAddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = conn.Write([]byte(state))
	return err
}

// NotifyReady signals systemd that the daemon has completed startup.
func NotifyReady() error {
	return Notify("READY=1")
}

// NotifyWatchdog sends a keepalive ping to systemd watchdog.
func NotifyWatchdog() error {
	return Notify("WATCHDOG=1")
}

// NotifyStopping signals systemd that the daemon is shutting down cleanly.
func NotifyStopping() error {
	return Notify("STOPPING=1")
}

// StartWatchdog starts a background keepalive goroutine if WATCHDOG_USEC is configured.
func StartWatchdog(ctx context.Context) {
	usecStr := os.Getenv("WATCHDOG_USEC")
	if usecStr == "" {
		return
	}

	usec, err := strconv.ParseInt(usecStr, 10, 64)
	if err != nil || usec <= 0 {
		return
	}

	// Ping at half the watchdog interval to avoid timeouts under load
	interval := time.Duration(usec/2) * time.Microsecond
	if interval < 100*time.Millisecond {
		interval = 100 * time.Millisecond
	}

	slog.Info("Systemd watchdog keepalive active", "interval", interval.String())
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = NotifyWatchdog()
			}
		}
	}()
}
