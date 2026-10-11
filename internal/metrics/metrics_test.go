package metrics

import (
	"testing"
	"time"
)

func TestMetrics_RecordAndSnapshot(t *testing.T) {
	r := NewRegistry()

	r.RecordSync("google", 150*time.Millisecond, true)
	r.RecordSync("microsoft", 250*time.Millisecond, false)
	r.RecordQueue(3, 1, 0)
	r.RecordDatabaseSize(1024 * 1024)
	r.RecordBackup(time.Now().Add(-10 * time.Minute))

	snap := r.Snapshot()
	if snap.SyncCount != 2 {
		t.Errorf("expected 2 syncs, got %d", snap.SyncCount)
	}
	if snap.SyncFailures != 1 {
		t.Errorf("expected 1 sync failure, got %d", snap.SyncFailures)
	}
	if snap.ProviderFailures["microsoft"] != 1 {
		t.Errorf("expected 1 microsoft failure, got %d", snap.ProviderFailures["microsoft"])
	}
	if snap.QueuePending != 3 || snap.QueueRetry != 1 {
		t.Errorf("unexpected queue metrics: %+v", snap)
	}
	if snap.DatabaseSizeBytes != 1024*1024 {
		t.Errorf("unexpected db size: %d", snap.DatabaseSizeBytes)
	}
	if snap.BackupAgeSeconds < 590 || snap.BackupAgeSeconds > 610 {
		t.Errorf("expected backup age around 600s, got %d", snap.BackupAgeSeconds)
	}
	if snap.DiskTotalBytes == 0 {
		t.Logf("disk total bytes is 0 (may occur in restricted test envs)")
	}
	if snap.MemoryAllocBytes == 0 || snap.MemorySysBytes == 0 {
		t.Errorf("live runtime memory metrics were not populated: %+v", snap)
	}
}

func TestDefaultRegistry(t *testing.T) {
	d := Default()
	if d == nil {
		t.Fatal("expected non-nil default registry")
	}
}
