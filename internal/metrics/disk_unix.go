//go:build !windows

package metrics

import (
	"syscall"
)

func getDiskSpace() (freeBytes, totalBytes uint64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(".", &stat); err != nil {
		return 0, 0
	}
	// Bsize is int64 or int32 depending on platform; cast safely
	bsize := uint64(stat.Bsize)
	free := stat.Bavail * bsize
	total := stat.Blocks * bsize
	return free, total
}
