package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/hardware"
	qrcode "github.com/skip2/go-qrcode"
)

// handleAPIGetHardware returns hardware specifications and real-time thermals/metrics.
func (s *Server) handleAPIGetHardware(w http.ResponseWriter, r *http.Request) {
	info := s.hwMgr.GetSystemInfo(r.Context())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(info)
}

// handleAPIGetWifi returns the current Wi-Fi adapter connection status.
func (s *Server) handleAPIGetWifi(w http.ResponseWriter, r *http.Request) {
	status := s.hwMgr.Wifi().GetStatus(r.Context())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

// handleAPIScanWifi scans for nearby wireless access points.
func (s *Server) handleAPIScanWifi(w http.ResponseWriter, r *http.Request) {
	aps, err := s.hwMgr.Wifi().ScanNetworks(r.Context())
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(aps)
}

type connectWifiRequest struct {
	SSID     string `json:"ssid"`
	Password string `json:"password"`
}

// handleAPIConnectWifi joins an access point.
func (s *Server) handleAPIConnectWifi(w http.ResponseWriter, r *http.Request) {
	var req connectWifiRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}

	req.SSID = strings.TrimSpace(req.SSID)
	if req.SSID == "" {
		http.Error(w, `{"error":"ssid is required"}`, http.StatusBadRequest)
		return
	}

	if err := s.hwMgr.Wifi().Connect(r.Context(), req.SSID, req.Password); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"ssid":    req.SSID,
	})
}

type toggleHotspotRequest struct {
	Enable bool `json:"enable"`
}

// handleAPIToggleHotspot turns on or off the FamilyBoard-Setup AP hotspot.
func (s *Server) handleAPIToggleHotspot(w http.ResponseWriter, r *http.Request) {
	var req toggleHotspotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}

	if err := s.hwMgr.Wifi().ToggleHotspot(r.Context(), req.Enable); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"hotspot_active": req.Enable,
	})
}

// handleAPIGetWifiQRCode renders a scannable PNG QR code for phone onboarding (WIFI:S:ssid;T:WPA;P:password;;).
func (s *Server) handleAPIGetWifiQRCode(w http.ResponseWriter, r *http.Request) {
	status := s.hwMgr.Wifi().GetStatus(r.Context())
	ssid := status.HotspotSSID
	if ssid == "" {
		ssid = "FamilyBoard-Setup"
	}
	const pass = "familyboard123"

	// Wi-Fi QR Code standard format: WIFI:S:<SSID>;T:WPA;P:<PASSWORD>;;
	payload := fmt.Sprintf("WIFI:S:%s;T:WPA;P:%s;;", escapeWifiQRValue(ssid), escapeWifiQRValue(pass))

	pngBytes, err := qrcode.Encode(payload, qrcode.Medium, 280)
	if err != nil {
		http.Error(w, `{"error":"failed to generate QR code"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(pngBytes)
}

func escapeWifiQRValue(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `;`, `\;`, `,`, `\,`, `:`, `\:`, `"`, `\"`)
	return replacer.Replace(value)
}

// handleAPIGetBluetooth returns Bluetooth adapter power and discovery state.
func (s *Server) handleAPIGetBluetooth(w http.ResponseWriter, r *http.Request) {
	status := s.hwMgr.Bluetooth().GetStatus(r.Context())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

type setBluetoothPowerRequest struct {
	Powered bool `json:"powered"`
}

// handleAPISetBluetoothPower powers on or off the Bluetooth adapter.
func (s *Server) handleAPISetBluetoothPower(w http.ResponseWriter, r *http.Request) {
	var req setBluetoothPowerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}

	if err := s.hwMgr.Bluetooth().SetPower(r.Context(), req.Powered); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"powered": req.Powered,
	})
}

// handleAPIScanBluetooth discovers nearby Bluetooth and BLE devices.
func (s *Server) handleAPIScanBluetooth(w http.ResponseWriter, r *http.Request) {
	devices, err := s.hwMgr.Bluetooth().ScanDevices(r.Context())
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(devices)
}

// handleAPIGetPresence returns household presence beacons for family members.
func (s *Server) handleAPIGetPresence(w http.ResponseWriter, r *http.Request) {
	beacons := s.hwMgr.Bluetooth().ListBeacons()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(beacons)
}

// handleAPIRegisterBeacon adds or updates a family member's device beacon.
func (s *Server) handleAPIRegisterBeacon(w http.ResponseWriter, r *http.Request) {
	var beacon hardware.FamilyBeacon
	if err := json.NewDecoder(r.Body).Decode(&beacon); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(beacon.PersonName) == "" || strings.TrimSpace(beacon.MACAddress) == "" {
		http.Error(w, `{"error":"person_name and mac_address are required"}`, http.StatusBadRequest)
		return
	}

	s.hwMgr.Bluetooth().RegisterBeacon(beacon)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"beacon":  beacon,
	})
}

// handleAPIDeleteBeacon removes a registered family beacon.
func (s *Server) handleAPIDeleteBeacon(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, `{"error":"beacon id is required"}`, http.StatusBadRequest)
		return
	}

	s.hwMgr.Bluetooth().RemoveBeacon(id)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

type systemPowerRequest struct {
	Action string `json:"action"` // "reboot" or "shutdown"
}

// handleAPISystemPower executes graceful reboot or shutdown on the Radxa Dragon.
func (s *Server) handleAPISystemPower(w http.ResponseWriter, r *http.Request) {
	var req systemPowerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}

	switch strings.ToLower(req.Action) {
	case "reboot":
		go func() {
			_ = s.hwMgr.Reboot(r.Context())
		}()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"action":  "reboot",
			"message": "Appliance is rebooting...",
		})
	case "shutdown", "poweroff":
		go func() {
			_ = s.hwMgr.PowerOff(r.Context())
		}()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"action":  "shutdown",
			"message": "Appliance is powering off...",
		})
	default:
		http.Error(w, `{"error":"action must be reboot or shutdown"}`, http.StatusBadRequest)
	}
}

func (s *Server) getPhotosDir() string {
	photosDir := filepath.Join(filepath.Dir(s.cfg.Database.Path), "photos")
	_ = os.MkdirAll(photosDir, 0755)
	return photosDir
}

// handleAPIGetPhotos returns available photo slideshow items for the FotoCube ambient screensaver.
func (s *Server) handleAPIGetPhotos(w http.ResponseWriter, r *http.Request) {
	photosDir := s.getPhotosDir()

	var photos []string
	if entries, err := os.ReadDir(photosDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				ext := strings.ToLower(filepath.Ext(e.Name()))
				if ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" {
					photos = append(photos, "/photos/"+e.Name())
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"photos": photos,
	})
}

// handleAPIUploadPhoto handles uploading photos for the FotoCube ambient slideshow.
func (s *Server) handleAPIUploadPhoto(w http.ResponseWriter, r *http.Request) {
	// Keep request bodies bounded before multipart parsing writes overflow to disk.
	r.Body = http.MaxBytesReader(w, r.Body, 33<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, `{"error":"file too large or invalid multipart form"}`, http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("photo")
	if err != nil {
		http.Error(w, `{"error":"missing 'photo' form field"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
		http.Error(w, `{"error":"only JPG, PNG, and WebP images are supported"}`, http.StatusBadRequest)
		return
	}

	photosDir := s.getPhotosDir()
	cleanName := filepath.Base(header.Filename)
	timestampedName := fmt.Sprintf("%d_%s", time.Now().UnixNano(), cleanName)
	dstPath := filepath.Join(photosDir, timestampedName)

	dst, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		http.Error(w, `{"error":"failed to save photo"}`, http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		_ = dst.Close()
		_ = os.Remove(dstPath)
		http.Error(w, `{"error":"failed to write photo content"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"filename": timestampedName,
		"url":      "/photos/" + timestampedName,
	})
}

// handleAPIDeletePhoto removes a photo from the FotoCube slideshow directory.
func (s *Server) handleAPIDeletePhoto(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	if filename == "" {
		http.Error(w, `{"error":"filename is required"}`, http.StatusBadRequest)
		return
	}

	cleanName := filepath.Base(filename)
	photosDir := s.getPhotosDir()
	targetPath := filepath.Join(photosDir, cleanName)

	if err := os.Remove(targetPath); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, `{"error":"photo not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"failed to delete photo"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"filename": cleanName,
	})
}

// handleAPIMigrateNVMe triggers copying the system to an NVMe SSD on Radxa Dragon.
func (s *Server) handleAPIMigrateNVMe(w http.ResponseWriter, r *http.Request) {
	out, err := s.hwMgr.MigrateToNVMe(r.Context())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
			"output":  out,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Migration to NVMe initiated or completed successfully.",
		"output":  out,
	})
}
