package hardware

import (
	"testing"
	"time"
)

func TestParseBluetoothScanTracksObservedDevices(t *testing.T) {
	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	output := "[NEW] Device AA:BB:CC:DD:EE:FF Taylor Phone\n[CHG] Device AA:BB:CC:DD:EE:FF RSSI: -58\n"
	devices := parseBluetoothScan(output, now)
	if len(devices) != 1 {
		t.Fatalf("expected one deduplicated sighting, got %d", len(devices))
	}
	if devices[0].MACAddress != "AA:BB:CC:DD:EE:FF" || devices[0].RSSI != -58 || !devices[0].LastSeen.Equal(now) {
		t.Fatalf("unexpected parsed sighting: %+v", devices[0])
	}
}
