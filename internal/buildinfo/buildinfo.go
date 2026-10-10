// Package buildinfo exposes build metadata shared by the daemon, management
// CLI, health endpoint, backups, and diagnostics.
package buildinfo

var (
	Version = "1.0.0-rc.1"
	Commit  = "none"
	Date    = "unknown"
)
