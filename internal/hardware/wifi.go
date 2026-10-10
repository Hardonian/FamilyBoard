package hardware

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// WifiStatus represents the current Wi-Fi adapter state and connection.
type WifiStatus struct {
	Interface     string `json:"interface"`
	Enabled       bool   `json:"enabled"`
	Connected     bool   `json:"connected"`
	SSID          string `json:"ssid"`
	IPAddress     string `json:"ip_address"`
	SignalPercent int    `json:"signal_percent"`
	MACAddress    string `json:"mac_address"`
	HotspotActive bool   `json:"hotspot_active"`
	HotspotSSID   string `json:"hotspot_ssid,omitempty"`
}

// WifiAccessPoint represents a scanned Wi-Fi network.
type WifiAccessPoint struct {
	SSID          string `json:"ssid"`
	SignalPercent int    `json:"signal_percent"`
	Security      string `json:"security"` // e.g. "WPA2", "WPA3", "Open"
	IsCurrent     bool   `json:"is_current"`
}

// WifiManager interfaces with NetworkManager (nmcli) on the Radxa Dragon.
type WifiManager struct {
	mu              sync.RWMutex
	isMock          bool
	cachedScan      []WifiAccessPoint
	lastScanTime    time.Time
	mockConnected   bool
	mockSSID        string
	mockHotspot     bool
	hotspotPassword string
}

// NewWifiManager creates a Wi-Fi subsystem controller.
func NewWifiManager(isMock bool) *WifiManager {
	return &WifiManager{
		isMock:          isMock,
		mockConnected:   true,
		mockSSID:        "FamilyHouse-5G",
		hotspotPassword: "familyboard123",
	}
}

// SetHotspotPassword configures the stable, per-installation setup hotspot key.
func (w *WifiManager) SetHotspotPassword(password string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if password != "" {
		w.hotspotPassword = password
	}
}

// GetStatus queries current Wi-Fi interface and connection status.
func (w *WifiManager) GetStatus(ctx context.Context) WifiStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.isMock || runtime.GOOS != "linux" {
		return WifiStatus{
			Interface:     "wlan0",
			Enabled:       true,
			Connected:     w.mockConnected,
			SSID:          w.mockSSID,
			IPAddress:     "192.168.1.142",
			SignalPercent: 88,
			MACAddress:    "b8:27:eb:d4:9a:11",
			HotspotActive: w.mockHotspot,
			HotspotSSID:   "FamilyBoard-Setup",
		}
	}

	// 1. Check nmcli general wifi status
	out, err := exec.CommandContext(ctx, "nmcli", "-t", "-f", "WIFI", "general").Output()
	wifiEnabled := true
	if err == nil && strings.TrimSpace(string(out)) == "disabled" {
		wifiEnabled = false
	}

	// 2. Query active connection details
	status := WifiStatus{
		Interface: "wlan0",
		Enabled:   wifiEnabled,
	}

	activeOut, err := exec.CommandContext(ctx, "nmcli", "-t", "-f", "DEVICE,TYPE,STATE,CONNECTION", "device").Output()
	if err == nil {
		lines := strings.Split(string(activeOut), "\n")
		for _, line := range lines {
			parts := splitNMTerseLine(line)
			if len(parts) >= 4 && parts[1] == "wifi" {
				status.Interface = parts[0]
				if parts[2] == "connected" {
					status.Connected = true
					status.SSID = parts[3]
				}
				break
			}
		}
	}

	// 3. Query IP address and signal if connected
	if status.Connected {
		ipOut, err := exec.CommandContext(ctx, "nmcli", "-t", "-f", "IP4.ADDRESS", "device", "show", status.Interface).Output()
		if err == nil {
			for _, line := range strings.Split(string(ipOut), "\n") {
				if strings.HasPrefix(line, "IP4.ADDRESS:") {
					addr := strings.TrimPrefix(line, "IP4.ADDRESS:")
					status.IPAddress = strings.Split(addr, "/")[0]
					break
				}
			}
		}

		// Query signal strength of active connection
		sigOut, err := exec.CommandContext(ctx, "nmcli", "-t", "-f", "IN-USE,SIGNAL", "device", "wifi", "list").Output()
		if err == nil {
			for _, line := range strings.Split(string(sigOut), "\n") {
				if strings.HasPrefix(line, "*:") {
					sigStr := strings.TrimPrefix(line, "*:")
					if val, err := strconv.Atoi(strings.TrimSpace(sigStr)); err == nil {
						status.SignalPercent = val
					}
					break
				}
			}
		}
	}

	// 4. Check for active AP hotspot
	hotspotOut, err := exec.CommandContext(ctx, "nmcli", "-t", "-f", "NAME,TYPE", "connection", "show", "--active").Output()
	if err == nil {
		for _, line := range strings.Split(string(hotspotOut), "\n") {
			if strings.Contains(strings.ToLower(line), "hotspot") || strings.Contains(line, "FamilyBoard-Setup") {
				status.HotspotActive = true
				status.HotspotSSID = "FamilyBoard-Setup"
				break
			}
		}
	}

	return status
}

// ScanNetworks scans nearby Wi-Fi access points.
func (w *WifiManager) ScanNetworks(ctx context.Context) ([]WifiAccessPoint, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Rate limit active scanning to once every 4 seconds to protect Wi-Fi driver
	if time.Since(w.lastScanTime) < 4*time.Second && len(w.cachedScan) > 0 {
		return w.cachedScan, nil
	}

	if w.isMock || runtime.GOOS != "linux" {
		w.lastScanTime = time.Now()
		w.cachedScan = []WifiAccessPoint{
			{SSID: "FamilyHouse-5G", SignalPercent: 92, Security: "WPA2", IsCurrent: w.mockConnected && w.mockSSID == "FamilyHouse-5G"},
			{SSID: "FamilyHouse-IoT", SignalPercent: 84, Security: "WPA2", IsCurrent: false},
			{SSID: "Kitchen-Extender", SignalPercent: 68, Security: "WPA3", IsCurrent: false},
			{SSID: "Neighbor-Guest", SignalPercent: 35, Security: "Open", IsCurrent: false},
		}
		return w.cachedScan, nil
	}

	// Trigger rescan
	_ = exec.CommandContext(ctx, "nmcli", "device", "wifi", "rescan").Run()

	out, err := exec.CommandContext(ctx, "nmcli", "-t", "-f", "IN-USE,SSID,SIGNAL,SECURITY", "device", "wifi", "list").Output()
	if err != nil {
		return w.cachedScan, fmt.Errorf("nmcli wifi scan failed: %w", err)
	}

	var results []WifiAccessPoint
	seen := make(map[string]bool)

	for _, line := range strings.Split(string(out), "\n") {
		parts := splitNMTerseLine(line)
		if len(parts) >= 4 {
			inUse := parts[0] == "*"
			ssid := strings.TrimSpace(parts[1])
			if ssid == "" || seen[ssid] {
				continue
			}
			seen[ssid] = true

			sig, _ := strconv.Atoi(parts[2])
			sec := parts[3]
			if sec == "" {
				sec = "Open"
			}

			results = append(results, WifiAccessPoint{
				SSID:          ssid,
				SignalPercent: sig,
				Security:      sec,
				IsCurrent:     inUse,
			})
		}
	}

	w.lastScanTime = time.Now()
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].IsCurrent != results[j].IsCurrent {
			return results[i].IsCurrent
		}
		return results[i].SignalPercent > results[j].SignalPercent
	})
	w.cachedScan = results
	return results, nil
}

// Connect joins an access point using the provided SSID and passphrase.
func (w *WifiManager) Connect(ctx context.Context, ssid, password string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	ssid = strings.TrimSpace(ssid)
	if ssid == "" {
		return fmt.Errorf("ssid cannot be empty")
	}

	if w.isMock || runtime.GOOS != "linux" {
		w.mockConnected = true
		w.mockSSID = ssid
		w.mockHotspot = false
		w.cachedScan = nil
		return nil
	}

	var cmd *exec.Cmd
	if password != "" {
		cmd = exec.CommandContext(ctx, "nmcli", "device", "wifi", "connect", ssid, "password", password)
	} else {
		cmd = exec.CommandContext(ctx, "nmcli", "device", "wifi", "connect", ssid)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %s (%w)", ssid, string(out), err)
	}
	w.cachedScan = nil

	return nil
}

// splitNMTerseLine parses nmcli's terse output while honoring backslash-escaped
// separators (for example an SSID named "Family\: Upstairs").
func splitNMTerseLine(line string) []string {
	fields := make([]string, 0, 4)
	var field strings.Builder
	escaped := false
	for _, r := range line {
		if escaped {
			field.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == ':' {
			fields = append(fields, field.String())
			field.Reset()
			continue
		}
		field.WriteRune(r)
	}
	if escaped {
		field.WriteRune('\\')
	}
	fields = append(fields, field.String())
	return fields
}

// ToggleHotspot enables or disables the plug-and-play onboarding hotspot AP.
func (w *WifiManager) ToggleHotspot(ctx context.Context, enable bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.isMock || runtime.GOOS != "linux" {
		w.mockHotspot = enable
		return nil
	}

	if enable {
		// Launch hotspot: FamilyBoard-Setup
		cmd := exec.CommandContext(ctx, "nmcli", "device", "wifi", "hotspot", "ifname", "wlan0", "ssid", "FamilyBoard-Setup", "password", w.hotspotPassword)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to start setup hotspot: %s (%w)", string(out), err)
		}
	} else {
		// Stop hotspot connection
		cmd := exec.CommandContext(ctx, "nmcli", "connection", "down", "Hotspot")
		_ = cmd.Run()
	}

	return nil
}
