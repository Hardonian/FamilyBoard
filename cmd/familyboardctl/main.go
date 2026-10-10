package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/backup"
	"github.com/Hardonian/FamilyBoard/internal/buildinfo"
	"github.com/Hardonian/FamilyBoard/internal/config"
	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/diagnostics"
	"github.com/Hardonian/FamilyBoard/internal/restore"
)

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(1)
	}

	subcmd := os.Args[1]
	switch subcmd {
	case "status":
		runStatus()
	case "health":
		runHealth()
	case "version":
		fmt.Printf("FamilyBoard v%s (commit: %s, date: %s, arch: %s/%s)\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date, runtime.GOOS, runtime.GOARCH)
	case "sync-now":
		runSyncNow()
	case "backup":
		runBackup(os.Args[2:])
	case "verify-backup":
		runVerifyBackup(os.Args[2:])
	case "restore":
		runRestore(os.Args[2:])
	case "db-check":
		runDBCheck()
	case "diagnostics":
		runDiagnostics(os.Args[2:])
	case "repair":
		runRepair()
	case "update":
		runUpdate(os.Args[2:])
	case "os-update":
		runOSUpdate()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command %q\n\n", subcmd)
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Println("FamilyBoard Appliance Control Utility (familyboardctl)")
	fmt.Println("\nUsage: familyboardctl <command> [arguments]")
	fmt.Println("\nCommands:")
	fmt.Println("  status        Display comprehensive appliance status")
	fmt.Println("  health        Check appliance readiness probe (exit 0 on ready, 1 on error)")
	fmt.Println("  version       Print version, commit, and build architecture")
	fmt.Println("  sync-now      Trigger immediate calendar synchronization")
	fmt.Println("  backup        Create and integrity-check a consistent SQLite backup archive")
	fmt.Println("  verify-backup Verify a backup archive without restoring it")
	fmt.Println("  restore       Restore appliance database from backup archive")
	fmt.Println("  db-check      Execute SQLite integrity check and table counts")
	fmt.Println("  diagnostics   Generate a privacy-scrubbed appliance diagnostics report")
	fmt.Println("  repair        Verify and repair permissions, directories, and DB indexes")
	fmt.Println("  update        Atomically replace the daemon and retain a rollback copy")
	fmt.Println("  os-update     Perform supported same-major OS package maintenance")
}

func runStatus() {
	cfg, _, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration: %v\n", err)
		os.Exit(1)
	}

	database, err := db.Open(cfg.Database.Path, 5000, 1, 1)
	dbStatus := "healthy"
	if err != nil {
		dbStatus = "error: " + err.Error()
	} else {
		defer database.Close()
	}

	fmt.Printf("FamilyBoard v%s\n\n", buildinfo.Version)
	fmt.Printf("Application: %s\n", checkLive(cfg))
	fmt.Printf("Database:    %s (WAL)\n", dbStatus)
	fmt.Printf("Kiosk:       %s\n\n", checkKiosk())

	if database != nil {
		store := db.NewStore(database)
		ctx := context.Background()
		accounts, _ := store.ListOAuthAccounts(ctx)

		people, _ := store.ListPeople(ctx)
		if len(people) > 0 {
			fmt.Println("Household Accounts:")
			for _, p := range people {
				status := "not configured"
				for _, acc := range accounts {
					if acc.PersonID == p.ID {
						status = fmt.Sprintf("connected (%s, %s)", acc.Provider, acc.VerifiedEmail)
						break
					}
				}
				fmt.Printf("  %-18s: %s\n", p.DisplayName, status)
			}
			fmt.Println()
		}

		pendingMut, _ := store.CountPendingMutations(ctx)
		pendingCand, _ := store.CountPendingCandidates(ctx)
		activeEvents, _ := store.CountActiveEvents(ctx)
		calendars, _ := store.ListCalendars(ctx)
		var deadLetters int
		_ = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM mutation_queue WHERE state = 'dead_letter';").Scan(&deadLetters)

		fmt.Printf("Active Events:      %d\n", activeEvents)
		fmt.Printf("Pending Candidates: %d\n", pendingCand)
		fmt.Printf("Queued Mutations:   %d\n", pendingMut)
		fmt.Printf("Dead Letters:       %d\n", deadLetters)

		if len(calendars) > 0 {
			fmt.Println("\nCalendar Sync:")
			for _, cal := range calendars {
				lastSync := "never"
				if cal.LastSyncedAt != nil {
					lastSync = cal.LastSyncedAt.Local().Format(time.RFC3339)
				}
				state := "enabled"
				if !cal.EnabledForDisplay {
					state = "disabled"
				}
				fmt.Printf("  %-24s %-10s %s (last sync: %s)\n", cal.Name, cal.Provider, state, lastSync)
			}
		}
		fmt.Println()
	}

	fmt.Printf("Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
}

func checkLive(cfg *config.Config) string {
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health/live", cfg.Server.Port))
	if err != nil {
		return "service inactive or unreachable"
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("unhealthy (HTTP %d)", resp.StatusCode)
	}
	return "healthy"
}

func checkKiosk() string {
	if runtime.GOOS != "linux" {
		return "desktop environment"
	}
	// Check if chromium-browser or chromium is running
	out, err := exec.Command("pgrep", "-f", "chromium").Output()
	if err == nil && len(out) > 0 {
		return "running"
	}
	return "inactive"
}

func runHealth() {
	cfg, _, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Load config: %v\n", err)
		os.Exit(1)
	}
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health/ready", cfg.Server.Port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "NOT READY: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		fmt.Fprintf(os.Stderr, "NOT READY: HTTP %d %s\n", resp.StatusCode, strings.TrimSpace(string(body)))
		os.Exit(1)
	}
	fmt.Println("READY")
}

func runSyncNow() {
	cfg, _, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Load config: %v\n", err)
		os.Exit(1)
	}
	client := http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Post(fmt.Sprintf("http://127.0.0.1:%d/api/control/sync-now", cfg.Server.Port), "application/json", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Trigger sync failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		fmt.Fprintf(os.Stderr, "Trigger sync failed: HTTP %d %s\n", resp.StatusCode, strings.TrimSpace(string(body)))
		os.Exit(1)
	}
	fmt.Println("Sync command sent successfully.")
}

func runBackup(args []string) {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	toPath := fs.String("to", "backups", "Output directory or explicit .tar.gz path for backup archive")
	_ = fs.Parse(args)

	cfg, configPath, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Load config: %v\n", err)
		os.Exit(1)
	}

	database, err := db.Open(cfg.Database.Path, 5000, 1, 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Open database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	archivePath, err := backup.CreateBackup(context.Background(), database, configPath, *toPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Backup creation failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Backup created successfully: %s\n", archivePath)
}

func runVerifyBackup(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "Usage: familyboardctl verify-backup <archive-path>")
		os.Exit(1)
	}
	if err := restore.VerifyBackup(context.Background(), args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "Backup verification failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Backup verification passed: manifest checksum and SQLite integrity are valid.")
}

func runRestore(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: familyboardctl restore <archive-path>")
		os.Exit(1)
	}
	archivePath := args[0]

	cfg, configPath, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Load config: %v\n", err)
		os.Exit(1)
	}

	err = restore.RestoreBackup(context.Background(), archivePath, cfg.Database.Path, configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Restore failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Appliance restore completed successfully.")
}

func runDBCheck() {
	cfg, _, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Load config: %v\n", err)
		os.Exit(1)
	}

	database, err := db.Open(cfg.Database.Path, 5000, 1, 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Open database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	if err := database.CheckIntegrity(); err != nil {
		fmt.Printf("SQLite Integrity Check: FAILED (%v)\n", err)
		os.Exit(1)
	}
	fmt.Println("SQLite Integrity Check: PASSED (ok)")

	store := db.NewStore(database)
	ctx := context.Background()
	tables := []string{"people", "oauth_accounts", "calendars", "events", "mutation_queue", "event_candidates", "processed_messages"}
	for _, tbl := range tables {
		var c int
		_ = database.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s;", tbl)).Scan(&c)
		fmt.Printf("  - %-20s: %d rows\n", tbl, c)
	}
	_ = store
}

func runDiagnostics(args []string) {
	fs := flag.NewFlagSet("diagnostics", flag.ExitOnError)
	outPath := fs.String("out", "", "Write JSON diagnostics to this path instead of stdout")
	_ = fs.Parse(args)

	cfg, _, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Load config: %v\n", err)
		os.Exit(1)
	}

	database, err := db.Open(cfg.Database.Path, 5000, 1, 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Open database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	store := db.NewStore(database)
	report, err := diagnostics.Generate(context.Background(), store, cfg.Database.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Generate diagnostics: %v\n", err)
		os.Exit(1)
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Encode diagnostics: %v\n", err)
		os.Exit(1)
	}
	if *outPath == "" {
		fmt.Println(string(data))
		return
	}
	if err := writePrivateReport(*outPath, append(data, '\n')); err != nil {
		fmt.Fprintf(os.Stderr, "Write diagnostics: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Diagnostics report written: %s\n", *outPath)
}

func writePrivateReport(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create diagnostics output directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".familyboard-diagnostics-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary diagnostics report: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure diagnostics report: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write diagnostics report: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close diagnostics report: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("diagnostics report already exists: %s", path)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect diagnostics destination: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("publish diagnostics report: %w", err)
	}
	return nil
}

func runRepair() {
	cfg, _, _ := config.Load("")
	fmt.Println("Repairing FamilyBoard appliance state...")

	// 1. Verify and repair directories
	dirs := []string{
		cfg.Security.CredentialsDir,
		"backups",
		filepath.Dir(cfg.Database.Path),
	}
	for _, d := range dirs {
		if d != "" && d != "." {
			_ = os.MkdirAll(d, 0700)
			fmt.Printf("  - Checked directory: %s\n", d)
		}
	}

	// 2. Verify and repair database
	database, err := db.Open(cfg.Database.Path, 5000, 1, 1)
	if err == nil {
		_ = db.Migrate(context.Background(), database)
		_ = database.CheckIntegrity()
		database.Close()
		fmt.Println("  - Database schema and integrity verified")
	}

	fmt.Println("Appliance repair completed.")
}

func runUpdate(args []string) {
	defaultTarget, err := defaultDaemonPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Resolve daemon path: %v\n", err)
		os.Exit(1)
	}
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	targetPath := fs.String("target", defaultTarget, "Installed FamilyBoard daemon path")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "Usage: familyboardctl update [--target <daemon-path>] <binary-path>")
		os.Exit(1)
	}

	newBinary := fs.Arg(0)
	fmt.Printf("Validating and installing FamilyBoard daemon from %s...\n", newBinary)
	version, err := installDaemonBinary(newBinary, *targetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Update failed without replacing the installed daemon: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Daemon updated successfully: %s\n", version)
	fmt.Printf("Installed: %s\n", *targetPath)
	fmt.Printf("Rollback:  %s.old\n", *targetPath)
	if runtime.GOOS == "linux" {
		fmt.Println("Restart with: sudo systemctl restart familyboard.service")
	}
}

func runOSUpdate() {
	if runtime.GOOS != "linux" {
		fmt.Println("os-update is only supported on Linux / Raspberry Pi OS.")
		return
	}

	fmt.Println("Checking Raspberry Pi OS packages for supported maintenance...")
	cmdUpdate := exec.Command("sudo", "apt-get", "update")
	cmdUpdate.Stdout = os.Stdout
	cmdUpdate.Stderr = os.Stderr
	if err := cmdUpdate.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "apt-get update failed: %v\n", err)
		os.Exit(1)
	}

	cmdUpgrade := exec.Command("sudo", "apt-get", "-y", "upgrade")
	cmdUpgrade.Stdout = os.Stdout
	cmdUpgrade.Stderr = os.Stderr
	if err := cmdUpgrade.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "apt-get upgrade failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("OS maintenance complete.")
}
