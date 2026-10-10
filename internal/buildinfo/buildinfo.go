// Package buildinfo exposes build metadata shared by the daemon, management
// CLI, health endpoint, backups, and diagnostics.
package buildinfo

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)
