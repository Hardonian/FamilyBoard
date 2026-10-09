package hardware

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// BluetoothStatus represents the onboard Bluetooth adapter state.
type BluetoothStatus struct {
	Powered      bool   `json:"powered"`
	Discoverable bool   `json:"discoverable"`
	Pairable     bool   `json:"pairable"`
	Name         string `json:"name"`
	MACAddress   string `json:"mac_address"`
}

// BluetoothDevice represents a discovered nearby Bluetooth / BLE device.
type BluetoothDevice struct {
	MACAddress string    `json:"mac_address"`
	Name       string    `json:"name"`
	RSSI       int       `json:"rssi"`
	Connected  bool      `json:"connected"`
	Paired     bool      `json:"paired"`
	LastSeen   time.Time `json:"last_seen"`
}

// FamilyBeacon represents a registered family member's phone or smartwatch for presence detection.
type FamilyBeacon struct {
	ID         string    `json:"id"`
	PersonName string    `json:"person_name"`
	DeviceName string    `json:"device_name"`
	MACAddress string    `json:"mac_address"`
	IsPresent  bool      `json:"is_present"`
	LastSeen   time.Time `json:"last_seen"`
	RSSI       int       `json:"rssi"`
}

// BluetoothManager interfaces with BlueZ (bluetoothctl) on the Radxa Dragon.
type BluetoothManager struct {
	mu           sync.RWMutex
	isMock       bool
	mockPowered  bool
	beacons      map[string]*FamilyBeacon
	recentScan   map[string]BluetoothDevice
	lastScanTime time.Time
}

// NewBluetoothManager creates a Bluetooth controller.
func NewBluetoothManager(isMock bool) *BluetoothManager {
	bm := &BluetoothManager{
		isMock:      isMock,
		mockPowered: true,
		beacons:     make(map[string]*FamilyBeacon),
		recentScan:  make(map[string]BluetoothDevice),
	}

	// Pre-populate default sample beacons for family members
	bm.beacons["beacon-1"] = &FamilyBeacon{
		ID:         "beacon-1",
		PersonName: "Alex",
		DeviceName: "Alex's Phone",
		MACAddress: "DC:A6:32:01:4F:18",
		IsPresent:  true,
		LastSeen:   time.Now().Add(-2 * time.Minute),
		RSSI:       -62,
	}
	bm.beacons["beacon-2"] = &FamilyBeacon{
		ID:         "beacon-2",
		PersonName: "Jordan",
		DeviceName: "Jordan's Watch",
		MACAddress: "F4:0F:24:88:B1:09",
		IsPresent:  false,
		LastSeen:   time.Now().Add(-4 * time.Hour),
		RSSI:       -92,
	}

	return bm
}

// GetStatus checks if the Radxa Dragon Bluetooth adapter is powered and ready.
func (b *BluetoothManager) GetStatus(ctx context.Context) BluetoothStatus {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.isMock || runtime.GOOS != "linux" {
		return BluetoothStatus{
			Powered:      b.mockPowered,
			Discoverable: false,
			Pairable:     true,
			Name:         "Radxa-Dragon-FamilyBoard",
			MACAddress:   "b8:27:eb:11:22:33",
		}
	}

	out, err := exec.CommandContext(ctx, "bluetoothctl", "show").Output()
	if err != nil {
		return BluetoothStatus{Powered: false}
	}

	status := BluetoothStatus{}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Controller ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				status.MACAddress = parts[1]
			}
		} else if strings.HasPrefix(line, "Name: ") {
			status.Name = strings.TrimPrefix(line, "Name: ")
		} else if strings.HasPrefix(line, "Powered: ") {
			status.Powered = strings.TrimPrefix(line, "Powered: ") == "yes"
		} else if strings.HasPrefix(line, "Discoverable: ") {
			status.Discoverable = strings.TrimPrefix(line, "Discoverable: ") == "yes"
		} else if strings.HasPrefix(line, "Pairable: ") {
			status.Pairable = strings.TrimPrefix(line, "Pairable: ") == "yes"
		}
	}

	return status
}

// SetPower toggles the Bluetooth controller power state.
func (b *BluetoothManager) SetPower(ctx context.Context, powered bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.isMock || runtime.GOOS != "linux" {
		b.mockPowered = powered
		return nil
	}

	arg := "power off"
	if powered {
		arg = "power on"
	}

	cmd := exec.CommandContext(ctx, "bluetoothctl", arg)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bluetoothctl %s failed: %s (%w)", arg, string(out), err)
	}

	return nil
}

// ScanDevices discovers nearby Bluetooth and BLE devices.
func (b *BluetoothManager) ScanDevices(ctx context.Context) ([]BluetoothDevice, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.isMock || runtime.GOOS != "linux" {
		now := time.Now()
		return []BluetoothDevice{
			{MACAddress: "DC:A6:32:01:4F:18", Name: "Alex's Phone", RSSI: -62, LastSeen: now},
			{MACAddress: "F4:0F:24:88:B1:09", Name: "Jordan's Watch", RSSI: -88, LastSeen: now.Add(-10 * time.Minute)},
			{MACAddress: "70:EE:50:23:44:11", Name: "Kitchen Soundbar", RSSI: -54, Paired: true, Connected: false, LastSeen: now},
			{MACAddress: "C8:38:70:99:A2:41", Name: "Smart Thermostat", RSSI: -72, LastSeen: now},
		}, nil
	}

	// Read known devices list from BlueZ
	out, err := exec.CommandContext(ctx, "bluetoothctl", "devices").Output()
	if err != nil {
		return nil, fmt.Errorf("bluetoothctl devices failed: %w", err)
	}

	var devices []BluetoothDevice
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		// Output format: Device <MAC> <Name...>
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "Device" {
			mac := fields[1]
			name := strings.Join(fields[2:], " ")
			devices = append(devices, BluetoothDevice{
				MACAddress: mac,
				Name:       name,
				RSSI:       -70,
				LastSeen:   time.Now(),
			})
		}
	}

	return devices, nil
}

// ListBeacons returns registered family presence beacons and current detection status.
func (b *BluetoothManager) ListBeacons() []FamilyBeacon {
	b.mu.RLock()
	defer b.mu.RUnlock()

	beacons := make([]FamilyBeacon, 0, len(b.beacons))
	for _, bcn := range b.beacons {
		// Device is present if seen within the last 15 minutes and signal is adequate
		recent := time.Since(bcn.LastSeen) < 15*time.Minute && bcn.RSSI > -85
		bcnCopy := *bcn
		bcnCopy.IsPresent = recent
		beacons = append(beacons, bcnCopy)
	}
	return beacons
}

// RegisterBeacon adds or updates a family member's device for household presence detection.
func (b *BluetoothManager) RegisterBeacon(beacon FamilyBeacon) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if beacon.ID == "" {
		beacon.ID = fmt.Sprintf("beacon-%d", time.Now().UnixNano())
	}
	beacon.LastSeen = time.Now()
	b.beacons[beacon.ID] = &beacon
}

// RemoveBeacon unregisters a device beacon.
func (b *BluetoothManager) RemoveBeacon(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.beacons, id)
}
