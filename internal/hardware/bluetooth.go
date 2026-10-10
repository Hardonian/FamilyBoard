package hardware

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
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
	mu          sync.RWMutex
	isMock      bool
	mockPowered bool
	beacons     map[string]*FamilyBeacon
	recentScan  map[string]BluetoothDevice
}

// NewBluetoothManager creates a Bluetooth controller.
func NewBluetoothManager(isMock bool) *BluetoothManager {
	bm := &BluetoothManager{
		isMock:      isMock,
		mockPowered: true,
		beacons:     make(map[string]*FamilyBeacon),
		recentScan:  make(map[string]BluetoothDevice),
	}

	// Development previews use sample presence data. Real appliances start empty
	// and only show family members explicitly registered by an administrator.
	if isMock {
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

	state := "off"
	if powered {
		state = "on"
	}

	cmd := exec.CommandContext(ctx, "bluetoothctl", "power", state)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bluetoothctl power %s failed: %s (%w)", state, strings.TrimSpace(string(out)), err)
	}

	return nil
}

// ScanDevices discovers nearby Bluetooth and BLE devices.
func (b *BluetoothManager) ScanDevices(ctx context.Context) ([]BluetoothDevice, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.isMock || runtime.GOOS != "linux" {
		now := time.Now()
		devices := []BluetoothDevice{
			{MACAddress: "DC:A6:32:01:4F:18", Name: "Alex's Phone", RSSI: -62, LastSeen: now},
			{MACAddress: "F4:0F:24:88:B1:09", Name: "Jordan's Watch", RSSI: -88, LastSeen: now.Add(-10 * time.Minute)},
			{MACAddress: "70:EE:50:23:44:11", Name: "Kitchen Soundbar", RSSI: -54, Paired: true, Connected: false, LastSeen: now},
			{MACAddress: "C8:38:70:99:A2:41", Name: "Smart Thermostat", RSSI: -72, LastSeen: now},
		}
		b.updateBeaconSightings(devices)
		return devices, nil
	}

	// Ask BlueZ for a bounded discovery pass, then read the resulting device list.
	// Discovery can fail when the adapter is unavailable; the devices command below
	// returns a useful error without leaving an unbounded scan process behind.
	scanOut, _ := exec.CommandContext(ctx, "bluetoothctl", "--timeout", "5", "scan", "on").CombinedOutput()
	sightings := parseBluetoothScan(string(scanOut), time.Now())

	// Read known and newly discovered devices from BlueZ.
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
	b.updateBeaconSightings(sightings)

	return devices, nil
}

func parseBluetoothScan(output string, observedAt time.Time) []BluetoothDevice {
	byMAC := make(map[string]BluetoothDevice)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		macIndex := -1
		for i, field := range fields {
			candidate := strings.Trim(field, "[]()")
			if _, err := net.ParseMAC(candidate); err == nil {
				macIndex = i
				fields[i] = strings.ToUpper(candidate)
				break
			}
		}
		if macIndex < 0 {
			continue
		}
		mac := fields[macIndex]
		device := byMAC[mac]
		device.MACAddress = mac
		device.LastSeen = observedAt
		if device.RSSI == 0 {
			device.RSSI = -70
		}
		for i, field := range fields {
			if strings.TrimSuffix(field, ":") == "RSSI" && i+1 < len(fields) {
				if rssi, err := strconv.Atoi(strings.Trim(fields[i+1], "[]")); err == nil {
					device.RSSI = rssi
				}
			}
		}
		if macIndex+1 < len(fields) && fields[macIndex+1] != "RSSI:" {
			device.Name = strings.Join(fields[macIndex+1:], " ")
		}
		byMAC[mac] = device
	}
	devices := make([]BluetoothDevice, 0, len(byMAC))
	for _, device := range byMAC {
		devices = append(devices, device)
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].MACAddress < devices[j].MACAddress })
	return devices
}

func (b *BluetoothManager) updateBeaconSightings(devices []BluetoothDevice) {
	for _, device := range devices {
		b.recentScan[strings.ToUpper(device.MACAddress)] = device
		for _, beacon := range b.beacons {
			if strings.EqualFold(beacon.MACAddress, device.MACAddress) {
				beacon.LastSeen = device.LastSeen
				beacon.RSSI = device.RSSI
			}
		}
	}
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
	sort.Slice(beacons, func(i, j int) bool {
		if beacons[i].PersonName == beacons[j].PersonName {
			return beacons[i].ID < beacons[j].ID
		}
		return beacons[i].PersonName < beacons[j].PersonName
	})
	return beacons
}

// RegisterBeacon adds or updates a family member's device for household presence detection.
func (b *BluetoothManager) RegisterBeacon(beacon FamilyBeacon) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if beacon.ID == "" {
		beacon.ID = fmt.Sprintf("beacon-%d", time.Now().UnixNano())
	}
	beacon.IsPresent = false
	beacon.LastSeen = time.Time{}
	beacon.RSSI = -127
	if observed, ok := b.recentScan[strings.ToUpper(beacon.MACAddress)]; ok {
		beacon.LastSeen = observed.LastSeen
		beacon.RSSI = observed.RSSI
	}
	b.beacons[beacon.ID] = &beacon
}

// ReplaceBeacons restores persisted registrations without inventing a current
// presence observation.
func (b *BluetoothManager) ReplaceBeacons(beacons []FamilyBeacon) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.beacons = make(map[string]*FamilyBeacon, len(beacons))
	for i := range beacons {
		beacon := beacons[i]
		beacon.IsPresent = false
		b.beacons[beacon.ID] = &beacon
	}
}

// RemoveBeacon unregisters a device beacon.
func (b *BluetoothManager) RemoveBeacon(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.beacons, id)
}
