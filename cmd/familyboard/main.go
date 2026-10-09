package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/calendar/google"
	"github.com/Hardonian/FamilyBoard/internal/config"
	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/oauth"
	"github.com/Hardonian/FamilyBoard/internal/queue"
	calsync "github.com/Hardonian/FamilyBoard/internal/sync"
	"github.com/Hardonian/FamilyBoard/internal/web"
)

var (
	version   = "0.9.0"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
	configPath := flag.String("config", "", "Path to configuration TOML file")
	portFlag := flag.Int("port", 0, "Port override for web server")
	showVersion := flag.Bool("version", false, "Print version information and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("FamilyBoard v%s (commit: %s, date: %s)\n", version, commit, buildDate)
		os.Exit(0)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	slog.Info("Starting FamilyBoard appliance daemon", "version", version, "commit", commit)

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
		msOAuth = oauth.NewMicrosoftClient(cfg.Microsoft.ClientID, cfg.Microsoft.ClientSecret, cfg.Microsoft.RedirectURL)
	}

	googleCalClient := google.NewGoogleCalendarClient("", nil)

	// 6. Engines: Queue & Sync
	queueEngine := queue.NewEngine(store, nil, cfg.Sync.MaxMutationAttempts, time.Duration(cfg.Sync.MutationRetryIntervalSeconds)*time.Second)
	queueEngine.RegisterProvider(googleCalClient)

	syncEngine := calsync.NewEngine(store, nil, time.Duration(cfg.Sync.CalendarIntervalSeconds)*time.Second)
	syncEngine.RegisterProvider(googleCalClient)

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

	// 8. Start Background Workers
	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()

	queueEngine.Start(appCtx)
	syncEngine.Start(appCtx)

	// 9. HTTP Server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      server,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("FamilyBoard listening", "address", addr, "kiosk_url", fmt.Sprintf("http://%s/", addr))
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server failed", "err", err)
			os.Exit(1)
		}
	}()

	// 10. Graceful Shutdown on Signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	slog.Info("Shutting down FamilyBoard appliance...")
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()

	_ = httpServer.Shutdown(shutdownCtx)
	queueEngine.Stop()
	syncEngine.Stop()

	_ = database.Checkpoint()
	slog.Info("FamilyBoard stopped cleanly")
}
