package metrics

import (
	"sync"
	"time"
)

type MetricsSnapshot struct {
	StartTime          time.Time        `json:"start_time"`
	UptimeSeconds      int64            `json:"uptime_seconds"`
	SyncCount          int64            `json:"sync_count"`
	SyncFailures       int64            `json:"sync_failures"`
	LastSyncDurationMs int64            `json:"last_sync_duration_ms"`
	AvgSyncDurationMs  int64            `json:"avg_sync_duration_ms"`
	ProviderFailures   map[string]int64 `json:"provider_failures"`
	QueuePending       int              `json:"queue_pending"`
	QueueRetry         int              `json:"queue_retry"`
	QueueDeadLetter    int              `json:"queue_dead_letter"`
	DatabaseSizeBytes  int64            `json:"database_size_bytes"`
	BackupAgeSeconds   int64            `json:"backup_age_seconds"`
	DiskFreeBytes      uint64           `json:"disk_free_bytes"`
	DiskTotalBytes     uint64           `json:"disk_total_bytes"`
	DiskUsedPercent    float64          `json:"disk_used_percent"`
	MemoryAllocBytes   uint64           `json:"memory_alloc_bytes"`
}

type Registry struct {
	mu                sync.RWMutex
	startTime         time.Time
	syncCount         int64
	syncFailures      int64
	totalSyncDuration time.Duration
	lastSyncDuration  time.Duration
	providerFailures  map[string]int64
	queuePending      int
	queueRetry        int
	queueDeadLetter   int
	databaseSizeBytes int64
	lastBackupTime    time.Time
}

var defaultRegistry = NewRegistry()

func Default() *Registry {
	return defaultRegistry
}

func NewRegistry() *Registry {
	return &Registry{
		startTime:        time.Now(),
		providerFailures: make(map[string]int64),
	}
}

func (r *Registry) RecordSync(provider string, d time.Duration, success bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.syncCount++
	r.totalSyncDuration += d
	r.lastSyncDuration = d
	if !success {
		r.syncFailures++
		if provider != "" {
			r.providerFailures[provider]++
		}
	}
}

func (r *Registry) RecordQueue(pending, retry, deadLetter int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queuePending = pending
	r.queueRetry = retry
	r.queueDeadLetter = deadLetter
}

func (r *Registry) RecordDatabaseSize(bytes int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.databaseSizeBytes = bytes
}

func (r *Registry) RecordBackup(t time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastBackupTime = t
}

func (r *Registry) Snapshot() MetricsSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now()
	uptime := int64(now.Sub(r.startTime).Seconds())

	var avgMs int64
	if r.syncCount > 0 {
		avgMs = (r.totalSyncDuration / time.Duration(r.syncCount)).Milliseconds()
	}

	failuresCopy := make(map[string]int64, len(r.providerFailures))
	for k, v := range r.providerFailures {
		failuresCopy[k] = v
	}

	var backupAge int64 = -1
	if !r.lastBackupTime.IsZero() {
		backupAge = int64(now.Sub(r.lastBackupTime).Seconds())
	}

	diskFree, diskTotal := getDiskSpace()
	var diskUsedPct float64
	if diskTotal > 0 {
		diskUsedPct = float64(diskTotal-diskFree) / float64(diskTotal) * 100.0
	}

	return MetricsSnapshot{
		StartTime:          r.startTime,
		UptimeSeconds:      uptime,
		SyncCount:          r.syncCount,
		SyncFailures:       r.syncFailures,
		LastSyncDurationMs: r.lastSyncDuration.Milliseconds(),
		AvgSyncDurationMs:  avgMs,
		ProviderFailures:   failuresCopy,
		QueuePending:       r.queuePending,
		QueueRetry:         r.queueRetry,
		QueueDeadLetter:    r.queueDeadLetter,
		DatabaseSizeBytes:  r.databaseSizeBytes,
		BackupAgeSeconds:   backupAge,
		DiskFreeBytes:      diskFree,
		DiskTotalBytes:     diskTotal,
		DiskUsedPercent:    diskUsedPct,
	}
}
