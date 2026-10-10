package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/buildinfo"
	appleCal "github.com/Hardonian/FamilyBoard/internal/calendar/apple"
	"github.com/Hardonian/FamilyBoard/internal/calendar/google"
	msCal "github.com/Hardonian/FamilyBoard/internal/calendar/microsoft"
	"github.com/Hardonian/FamilyBoard/internal/config"
	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/oauth"
	"github.com/Hardonian/FamilyBoard/internal/queue"
	calsync "github.com/Hardonian/FamilyBoard/internal/sync"
	"github.com/Hardonian/FamilyBoard/internal/web"
)

func main() {
	configPath := flag.String("config", "", "Path to configuration TOML file")
	portFlag := flag.Int("port", 0, "Port override for web server")
	showVersion := flag.Bool("version", false, "Print version information and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("FamilyBoard v%s (commit: %s, date: %s)\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date)
		os.Exit(0)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	slog.Info("Starting FamilyBoard appliance daemon", "version", buildinfo.Version, "commit", buildinfo.Commit)

	// 1. Load Configuration
	cfg, loadedPath, err := config.Load(*configPath)
	if err != nil {
		slog.Error("Failed to load configuration", "err", err)
		os.Exit(1)
	}
	if loadedPath != "" {
		slog.Info("Loaded configuration", "path", loadedPath)
	} else {
		slog.Info("Using default configuration")
	}
	if cfg.Security.SessionSecret == "" || cfg.Security.HotspotPassword == "" {
		if err := cfg.EnsureSessionSecret(); err != nil {
			slog.Error("Failed to generate session secret", "err", err)
			os.Exit(1)
		}
		if err := cfg.EnsureHotspotPassword(); err != nil {
			slog.Error("Failed to generate hotspot password", "err", err)
			os.Exit(1)
		}
		savePath := loadedPath
		if savePath == "" || savePath == "config.example.toml" {
			savePath = "config.toml"
		}
		if *configPath != "" {
			savePath = *configPath
		}
		if err := cfg.Save(savePath); err != nil {
			slog.Error("Failed to persist generated session secret", "path", savePath, "err", err)
			os.Exit(1)
		}
		slog.Info("Generated and saved installation security secrets", "path", savePath)
	}

	if *portFlag > 0 {
		cfg.Server.Port = *portFlag
	}

	// 2. Open SQLite Database
	database, err := db.Open(cfg.Database.Path, cfg.Database.BusyTimeoutMs, cfg.Database.MaxOpenConns, cfg.Database.MaxIdleConns)
	if err != nil {
		slog.Error("Failed to open SQLite database", "path", cfg.Database.Path, "err", err)
		os.Exit(1)
	}
	defer database.Close()

	// 3. Apply Schema Migrations
	ctx := context.Background()
	if err := db.Migrate(ctx, database); err != nil {
		slog.Error("Database migration failed", "err", err)
		os.Exit(1)
	}
	ver, _ := db.CurrentVersion(ctx, database)
	slog.Info("Database initialized successfully", "path", cfg.Database.Path, "schema_version", ver)

	store := db.NewStore(database)

	// 4. Token Storage & State Manager
	tokenStorage, err := oauth.NewTokenStorage(cfg.Security.CredentialsDir)
	if err != nil {
		slog.Error("Failed to initialize token storage", "err", err)
		os.Exit(1)
	}
	stateManager := oauth.NewStateManager(cfg.Security.SessionSecret)

	// 5. Providers & Clients
	googleOAuth := oauth.NewGoogleClient(cfg.Google.ClientID, cfg.Google.ClientSecret, cfg.Google.RedirectURL)
	var msOAuth *oauth.MicrosoftClient
	if cfg.Microsoft.Enabled {
		msOAuth = oauth.NewMicrosoftClient(cfg.Microsoft.ClientID, cfg.Microsoft.ClientSecret, cfg.Microsoft.RedirectURL, cfg.Microsoft.TenantID)
	}

	googleCalClient := google.NewGoogleCalendarClient("", nil)
	msCalClient := msCal.NewCalendarClient("", nil)
	appleCalClient := appleCal.NewAppleCalendarClient("", nil)

	// 6. Engines: Queue & Sync
	queueEngine := queue.NewEngine(store, nil, cfg.Sync.MaxMutationAttempts, time.Duration(cfg.Sync.MutationRetryIntervalSeconds)*time.Second)
	queueEngine.RegisterProvider(googleCalClient)
	queueEngine.RegisterProvider(msCalClient)
	queueEngine.RegisterProvider(appleCalClient)

	syncEngine := calsync.NewEngine(store, nil, time.Duration(cfg.Sync.CalendarIntervalSeconds)*time.Second)
	syncEngine.RegisterProvider(googleCalClient)
	syncEngine.RegisterProvider(msCalClient)
	syncEngine.RegisterProvider(appleCalClient)

	// 7. Web Server
	server, err := web.NewServer(cfg, store, tokenStorage, stateManager, googleOAuth, msOAuth, syncEngine, queueEngine)
	if err != nil {
		slog.Error("Failed to initialize web server", "err", err)
		os.Exit(1)
	}

	// Wire token provider from server to engines
	// (server implements queue.TokenProvider)
	queueEngine.SetTokenProvider(server)
	syncEngine.SetTokenProvider(server)
	syncEngine.SetOnSyncCompleted(server.BroadcastReload)

	// Bring up the per-installation setup hotspot whenever the appliance has no
	// working Wi-Fi connection, keeping first boot and recovery phone-friendly.
	wifiCtx, cancelWifi := context.WithTimeout(context.Background(), 20*time.Second)
	wifiStatus := server.HardwareManager().Wifi().GetStatus(wifiCtx)
	if !wifiStatus.Connected && !wifiStatus.HotspotActive {
		if err := server.HardwareManager().Wifi().ToggleHotspot(wifiCtx, true); err != nil {
			slog.Warn("Could not start setup hotspot", "err", err)
		} else {
			slog.Info("Started setup hotspot", "ssid", "FamilyBoard-Setup")
		}
	}
	cancelWifi()

	// 8. Start Background Workers
	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()

	queueEngine.Start(appCtx)
	syncEngine.Start(appCtx)

	// 9. HTTP Server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           server,
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	serveErrCh := make(chan error, 1)
	go func() {
		slog.Info("FamilyBoard listening", "address", addr, "kiosk_url", fmt.Sprintf("http://%s/", addr))
		err := httpServer.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErrCh <- err
	}()

	// 10. Graceful Shutdown on Signals or a listener failure.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	var serveFailure error
	select {
	case sig := <-sigCh:
		slog.Info("Received shutdown signal", "signal", sig.String())
	case serveFailure = <-serveErrCh:
		if serveFailure != nil {
			slog.Error("HTTP server failed", "err", serveFailure)
		}
	}

	slog.Info("Shutting down FamilyBoard appliance...")
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()

	if serveFailure == nil {
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Warn("HTTP server shutdown did not complete cleanly", "err", err)
		}
	}
	cancelApp()
	queueEngine.Stop()
	syncEngine.Stop()

	if err := database.Checkpoint(); err != nil {
		slog.Warn("Database checkpoint failed during shutdown", "err", err)
	}
	slog.Info("FamilyBoard stopped cleanly")
	if serveFailure != nil {
		// Explicitly close before os.Exit, which does not run deferred calls.
		_ = database.Close()
		os.Exit(1)
	}
}
