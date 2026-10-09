package hardware

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SystemInfo holds appliance hardware metrics and platform identity.
type SystemInfo struct {
	HardwareModel   string  `json:"hardware_model"`
	IsRadxaDragon   bool    `json:"is_radxa_dragon"`
	Architecture    string  `json:"architecture"`
	Kernel          string  `json:"kernel"`
	NumCPU          int     `json:"num_cpu"`
	CPUTempCelsius  float64 `json:"cpu_temp_celsius"`
	MemoryTotalMB   uint64  `json:"memory_total_mb"`
	MemoryUsedMB    uint64  `json:"memory_used_mb"`
	MemoryPercent   float64 `json:"memory_percent"`
	DiskTotalGB     float64 `json:"disk_total_gb"`
	DiskUsedGB      float64 `json:"disk_used_gb"`
	DiskPercent     float64 `json:"disk_percent"`
	UptimeSeconds   int64   `json:"uptime_seconds"`
	WifiConnected   bool    `json:"wifi_connected"`
	WifiSSID        string  `json:"wifi_ssid"`
	BluetoothActive bool    `json:"bluetooth_active"`
	BootMedia       string  `json:"boot_media"`
	NVMeDetected    bool    `json:"nvme_detected"`
	NVMeModel       string  `json:"nvme_model,omitempty"`
	NVMeCapacityGB  float64 `json:"nvme_capacity_gb,omitempty"`
}

// Manager coordinates hardware interactions including Wi-Fi, Bluetooth, and device power.
type Manager struct {
	mu           sync.RWMutex
	wifi         *WifiManager
	bluetooth    *BluetoothManager
	isMock       bool
	lastScanTime time.Time
}

// NewManager creates an appliance hardware coordinator.
func NewManager() *Manager {
	m := &Manager{
		isMock: runtime.GOOS != "linux",
	}
	m.wifi = NewWifiManager(m.isMock)
	m.bluetooth = NewBluetoothManager(m.isMock)
	return m
}

// Wifi returns the Wi-Fi subsystem manager.
func (m *Manager) Wifi() *WifiManager {
	return m.wifi
}

// Bluetooth returns the Bluetooth subsystem manager.
func (m *Manager) Bluetooth() *BluetoothManager {
	return m.bluetooth
}

// GetSystemInfo inspects the underlying hardware (e.g. Radxa Dragon Q64).
func (m *Manager) GetSystemInfo(ctx context.Context) SystemInfo {
	hwModel := "Radxa Dragon Q64 Appliance"
	isRadxa := false

	if runtime.GOOS == "linux" {
		if modelBytes, err := os.ReadFile("/proc/device-tree/model"); err == nil {
			raw := strings.TrimRight(string(modelBytes), "\x00\n\r")
			if raw != "" {
				hwModel = raw
			}
		}
		lower := strings.ToLower(hwModel)
		if strings.Contains(lower, "radxa") || strings.Contains(lower, "dragon") || strings.Contains(lower, "qcs6490") {
			isRadxa = true
		}
	} else {
		hwModel = fmt.Sprintf("Development Host (%s/%s)", runtime.GOOS, runtime.GOARCH)
	}

	cpuTemp := m.readCPUTemp()
	memTotal, memUsed, memPct := m.readMemory()
	diskTotal, diskUsed, diskPct := m.readDisk()
	uptime := m.readUptime()

	wifiStatus := m.wifi.GetStatus(ctx)
	btStatus := m.bluetooth.GetStatus(ctx)
	bootMedia, nvmeDetected, nvmeModel, nvmeCap := m.inspectStorageMedia()

	return SystemInfo{
		HardwareModel:   hwModel,
		IsRadxaDragon:   isRadxa || runtime.GOARCH == "arm64",
		Architecture:    runtime.GOARCH,
		Kernel:          runtime.GOOS,
		NumCPU:          runtime.NumCPU(),
		CPUTempCelsius:  cpuTemp,
		MemoryTotalMB:   memTotal,
		MemoryUsedMB:    memUsed,
		MemoryPercent:   memPct,
		DiskTotalGB:     diskTotal,
		DiskUsedGB:      diskUsed,
		DiskPercent:     diskPct,
		UptimeSeconds:   uptime,
		WifiConnected:   wifiStatus.Connected,
		WifiSSID:        wifiStatus.SSID,
		BluetoothActive: btStatus.Powered,
		BootMedia:       bootMedia,
		NVMeDetected:    nvmeDetected,
		NVMeModel:       nvmeModel,
		NVMeCapacityGB:  nvmeCap,
	}
}

func (m *Manager) readCPUTemp() float64 {
	if runtime.GOOS != "linux" {
		return 38.5 // Simulated normal temperature for dev
	}

	thermalPaths := []string{
		"/sys/class/thermal/thermal_zone0/temp",
		"/sys/class/thermal/thermal_zone1/temp",
		"/sys/class/hwmon/hwmon0/temp1_input",
	}

	for _, p := range thermalPaths {
		if data, err := os.ReadFile(p); err == nil {
			rawStr := strings.TrimSpace(string(data))
			if millidegrees, err := strconv.ParseFloat(rawStr, 64); err == nil && millidegrees > 0 {
				if millidegrees > 1000 {
					return millidegrees / 1000.0
				}
				return millidegrees
			}
		}
	}
	return 0
}

func (m *Manager) readMemory() (totalMB uint64, usedMB uint64, pct float64) {
	if runtime.GOOS != "linux" {
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)
		return 4096, memStats.Alloc / (1024 * 1024), float64(memStats.Alloc) / (4096 * 1024 * 1024) * 100
	}

	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, 0
	}

	var memTotalKB, memAvailableKB uint64
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			switch fields[0] {
			case "MemTotal:":
				memTotalKB, _ = strconv.ParseUint(fields[1], 10, 64)
			case "MemAvailable:":
				memAvailableKB, _ = strconv.ParseUint(fields[1], 10, 64)
			}
		}
	}

	if memTotalKB > 0 {
		totalMB = memTotalKB / 1024
		usedKB := memTotalKB - memAvailableKB
		usedMB = usedKB / 1024
		pct = (float64(usedKB) / float64(memTotalKB)) * 100.0
	}
	return totalMB, usedMB, pct
}

func (m *Manager) readDisk() (totalGB float64, usedGB float64, pct float64) {
	// Standard df command parsing on Unix
	if runtime.GOOS == "linux" {
		out, err := exec.Command("df", "-B1", "/").Output()
		if err == nil {
			lines := strings.Split(string(out), "\n")
			if len(lines) >= 2 {
				fields := strings.Fields(lines[1])
				if len(fields) >= 4 {
					totalBytes, _ := strconv.ParseFloat(fields[1], 64)
					usedBytes, _ := strconv.ParseFloat(fields[2], 64)
					if totalBytes > 0 {
						totalGB = totalBytes / (1024 * 1024 * 1024)
						usedGB = usedBytes / (1024 * 1024 * 1024)
						pct = (usedBytes / totalBytes) * 100.0
						return totalGB, usedGB, pct
					}
				}
			}
		}
	}
	return 64.0, 4.2, 6.56 // Simulated 64GB eMMC storage
}

func (m *Manager) readUptime() int64 {
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile("/proc/uptime")
		if err == nil {
			parts := strings.Fields(string(data))
			if len(parts) > 0 {
				if val, err := strconv.ParseFloat(parts[0], 64); err == nil {
					return int64(val)
				}
			}
		}
	}
	return 1200
}

// Reboot requests an operating system reboot via systemd / shutdown command.
func (m *Manager) Reboot(ctx context.Context) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	cmd := exec.CommandContext(ctx, "systemctl", "reboot")
	if err := cmd.Run(); err != nil {
		return exec.CommandContext(ctx, "reboot").Run()
	}
	return nil
}

// PowerOff requests an operating system halt/shutdown.
func (m *Manager) PowerOff(ctx context.Context) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	cmd := exec.CommandContext(ctx, "systemctl", "poweroff")
	if err := cmd.Run(); err != nil {
		return exec.CommandContext(ctx, "poweroff").Run()
	}
	return nil
}

// inspectStorageMedia determines whether the system is running from MicroSD, eMMC, or NVMe SSD.
func (m *Manager) inspectStorageMedia() (bootMedia string, nvmeDetected bool, nvmeModel string, nvmeCapacityGB float64) {
	if runtime.GOOS != "linux" {
		return "MicroSD (Simulated)", true, "Samsung 980 NVMe SSD (M.2)", 500.0
	}

	bootMedia = "Local Disk"
	// Check root mount source from /proc/mounts
	if mounts, err := os.ReadFile("/proc/mounts"); err == nil {
		for _, line := range strings.Split(string(mounts), "\n") {
			parts := strings.Fields(line)
			if len(parts) >= 2 && parts[1] == "/" {
				dev := parts[0]
				if strings.Contains(dev, "nvme") {
					bootMedia = "NVMe SSD"
				} else if strings.Contains(dev, "mmcblk0") {
					bootMedia = "MicroSD Card"
				} else if strings.Contains(dev, "mmcblk1") {
					bootMedia = "eMMC Storage"
				}
				break
			}
		}
	}

	// Check if NVMe block device is present in the M.2 slot
	if _, err := os.Stat("/sys/block/nvme0n1"); err == nil {
		nvmeDetected = true
		nvmeModel = "M.2 NVMe SSD"
		if modelBytes, err := os.ReadFile("/sys/block/nvme0n1/device/model"); err == nil {
			nvmeModel = strings.TrimSpace(string(modelBytes))
		}
		if sizeBytes, err := os.ReadFile("/sys/block/nvme0n1/size"); err == nil {
			if sectors, err := strconv.ParseFloat(strings.TrimSpace(string(sizeBytes)), 64); err == nil && sectors > 0 {
				// Standard Linux block sectors are 512 bytes
				nvmeCapacityGB = (sectors * 512.0) / (1024 * 1024 * 1024)
			}
		}
	}

	return bootMedia, nvmeDetected, nvmeModel, nvmeCapacityGB
}

// MigrateToNVMe triggers the background migration script to clone the live system to an NVMe drive.
func (m *Manager) MigrateToNVMe(ctx context.Context) (string, error) {
	if runtime.GOOS != "linux" {
		return "Simulated migration to NVMe completed successfully.", nil
	}

	scriptPaths := []string{
		"/usr/local/bin/familyboard-migrate-nvme.sh",
		"/usr/local/bin/familyboard-migrate-nvme",
		"deploy/radxa/migrate-to-nvme.sh",
	}

	var targetScript string
	for _, p := range scriptPaths {
		if _, err := os.Stat(p); err == nil {
			targetScript = p
			break
		}
	}

	if targetScript == "" {
		return "", fmt.Errorf("NVMe migration script not found in standard system paths")
	}

	cmd := exec.CommandContext(ctx, "bash", targetScript, "--non-interactive")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("NVMe migration failed: %s (%w)", string(out), err)
	}

	return string(out), nil
}
