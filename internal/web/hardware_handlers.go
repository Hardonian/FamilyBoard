package web

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/auth"
	"github.com/Hardonian/FamilyBoard/internal/hardware"
	"github.com/Hardonian/FamilyBoard/internal/models"
	qrcode "github.com/skip2/go-qrcode"
	_ "golang.org/x/image/webp"
)

const maxJSONRequestBody = 64 << 10
const presenceBeaconsSettingKey = "presence_beacons"
const maxPhotoUploadBytes = 32 << 20
const maxPhotoLibraryBytes = 1 << 30
const maxPhotoPixels = 40_000_000
const maxPhotoDimension = 12_000

func decodeRequestJSON(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONRequestBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeHardwareError(w, http.StatusBadRequest, "invalid request payload")
		return false
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeHardwareError(w, http.StatusBadRequest, "request body must contain one JSON object")
		return false
	}
	return true
}

func writeHardwareError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

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
		writeHardwareError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(aps)
}

type connectWifiRequest struct {
	SSID     string `json:"ssid"`
	Password string `json:"password"`
}

func validWifiPassword(password string) bool {
	if len(password) >= 8 && len(password) <= 63 {
		return true
	}
	if len(password) != 64 {
		return false
	}
	for _, char := range password {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}

// handleAPIConnectWifi joins an access point.
func (s *Server) handleAPIConnectWifi(w http.ResponseWriter, r *http.Request) {
	var req connectWifiRequest
	if !decodeRequestJSON(w, r, &req) {
		return
	}

	if strings.TrimSpace(req.SSID) == "" {
		writeHardwareError(w, http.StatusBadRequest, "ssid is required")
		return
	}
	if len([]byte(req.SSID)) > 32 {
		writeHardwareError(w, http.StatusBadRequest, "ssid must be at most 32 bytes")
		return
	}
	if req.Password != "" && !validWifiPassword(req.Password) {
		writeHardwareError(w, http.StatusBadRequest, "password must be 8–63 characters or a 64-digit hexadecimal key")
		return
	}

	if err := s.hwMgr.Wifi().Connect(r.Context(), req.SSID, req.Password); err != nil {
		writeHardwareError(w, http.StatusInternalServerError, err.Error())
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
	if !decodeRequestJSON(w, r, &req) {
		return
	}

	if err := s.hwMgr.Wifi().ToggleHotspot(r.Context(), req.Enable); err != nil {
		writeHardwareError(w, http.StatusInternalServerError, err.Error())
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
	pass := s.cfg.Security.HotspotPassword

	// Wi-Fi QR Code standard format: WIFI:S:<SSID>;T:WPA;P:<PASSWORD>;;
	payload := fmt.Sprintf("WIFI:S:%s;T:WPA;P:%s;;", escapeWifiQRValue(ssid), escapeWifiQRValue(pass))

	pngBytes, err := qrcode.Encode(payload, qrcode.Medium, 280)
	if err != nil {
		writeHardwareError(w, http.StatusInternalServerError, "failed to generate QR code")
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
	if !decodeRequestJSON(w, r, &req) {
		return
	}

	if err := s.hwMgr.Bluetooth().SetPower(r.Context(), req.Powered); err != nil {
		writeHardwareError(w, http.StatusInternalServerError, err.Error())
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
		writeHardwareError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(devices)
}

type bluetoothDeviceActionRequest struct {
	MACAddress string `json:"mac_address"`
	MAC        string `json:"mac"`
}

func (r bluetoothDeviceActionRequest) resolvedMAC() string {
	mac := strings.TrimSpace(r.MACAddress)
	if mac == "" {
		mac = strings.TrimSpace(r.MAC)
	}
	return mac
}

// handleAPIConnectBluetooth pairs, trusts, and connects a Bluetooth device (e.g. keyboard).
func (s *Server) handleAPIConnectBluetooth(w http.ResponseWriter, r *http.Request) {
	var req bluetoothDeviceActionRequest
	if !decodeRequestJSON(w, r, &req) {
		return
	}
	mac := req.resolvedMAC()
	if mac == "" {
		writeHardwareError(w, http.StatusBadRequest, "mac_address is required")
		return
	}

	if err := s.hwMgr.Bluetooth().PairAndConnectDevice(r.Context(), mac); err != nil {
		writeHardwareError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"mac_address": mac,
		"connected":   true,
		"paired":      true,
	})
}

// handleAPIDisconnectBluetooth disconnects an active Bluetooth device connection.
func (s *Server) handleAPIDisconnectBluetooth(w http.ResponseWriter, r *http.Request) {
	var req bluetoothDeviceActionRequest
	if !decodeRequestJSON(w, r, &req) {
		return
	}
	mac := req.resolvedMAC()
	if mac == "" {
		writeHardwareError(w, http.StatusBadRequest, "mac_address is required")
		return
	}

	if err := s.hwMgr.Bluetooth().DisconnectDevice(r.Context(), mac); err != nil {
		writeHardwareError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"mac_address": mac,
		"connected":   false,
	})
}

// handleAPIRemoveBluetooth unpairs and forgets a Bluetooth device.
func (s *Server) handleAPIRemoveBluetooth(w http.ResponseWriter, r *http.Request) {
	var req bluetoothDeviceActionRequest
	if !decodeRequestJSON(w, r, &req) {
		return
	}
	mac := req.resolvedMAC()
	if mac == "" {
		writeHardwareError(w, http.StatusBadRequest, "mac_address is required")
		return
	}

	if err := s.hwMgr.Bluetooth().RemoveDevice(r.Context(), mac); err != nil {
		writeHardwareError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"mac_address": req.MACAddress,
		"removed":     true,
	})
}

// handleAPIGetPresence returns household presence beacons for family members.
func (s *Server) handleAPIGetPresence(w http.ResponseWriter, r *http.Request) {
	scanCtx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	_, _ = s.hwMgr.Bluetooth().ScanDevices(scanCtx)
	cancel()
	beacons := s.hwMgr.Bluetooth().ListBeacons()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(beacons)
}

// handleAPIRegisterBeacon adds or updates a family member's device beacon.
func (s *Server) handleAPIRegisterBeacon(w http.ResponseWriter, r *http.Request) {
	var beacon hardware.FamilyBeacon
	if !decodeRequestJSON(w, r, &beacon) {
		return
	}

	if strings.TrimSpace(beacon.PersonName) == "" || strings.TrimSpace(beacon.MACAddress) == "" {
		writeHardwareError(w, http.StatusBadRequest, "person_name and mac_address are required")
		return
	}
	if len(beacon.PersonName) > 100 || len(beacon.DeviceName) > 100 {
		writeHardwareError(w, http.StatusBadRequest, "person and device names must be at most 100 characters")
		return
	}
	parsedMAC, err := net.ParseMAC(beacon.MACAddress)
	if err != nil {
		writeHardwareError(w, http.StatusBadRequest, "mac_address is invalid")
		return
	}
	beacon.MACAddress = strings.ToUpper(parsedMAC.String())
	if beacon.ID == "" {
		beacon.ID = fmt.Sprintf("beacon-%d", time.Now().UnixNano())
	}

	s.hwMgr.Bluetooth().RegisterBeacon(beacon)
	if err := s.persistPresenceBeacons(r.Context()); err != nil {
		s.hwMgr.Bluetooth().RemoveBeacon(beacon.ID)
		writeHardwareError(w, http.StatusInternalServerError, "failed to save presence beacon")
		return
	}
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
		writeHardwareError(w, http.StatusBadRequest, "beacon id is required")
		return
	}

	s.hwMgr.Bluetooth().RemoveBeacon(id)
	if err := s.persistPresenceBeacons(r.Context()); err != nil {
		writeHardwareError(w, http.StatusInternalServerError, "failed to save presence beacon changes")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

func (s *Server) persistPresenceBeacons(ctx context.Context) error {
	encoded, err := json.Marshal(s.hwMgr.Bluetooth().ListBeacons())
	if err != nil {
		return err
	}
	return s.store.SetSetting(ctx, presenceBeaconsSettingKey, string(encoded))
}

func (s *Server) loadPresenceBeacons(ctx context.Context) {
	encoded, err := s.store.GetSetting(ctx, presenceBeaconsSettingKey)
	if err != nil || encoded == "" {
		return
	}
	var beacons []hardware.FamilyBeacon
	if json.Unmarshal([]byte(encoded), &beacons) == nil {
		s.hwMgr.Bluetooth().ReplaceBeacons(beacons)
	}
}

type systemPowerRequest struct {
	Action string `json:"action"` // "reboot" or "shutdown"
}

// handleAPISystemPower executes graceful reboot or shutdown on the Radxa Dragon.
func (s *Server) handleAPISystemPower(w http.ResponseWriter, r *http.Request) {
	var req systemPowerRequest
	if !decodeRequestJSON(w, r, &req) {
		return
	}

	switch strings.ToLower(req.Action) {
	case "reboot":
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = s.hwMgr.Reboot(ctx)
		}()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"action":  "reboot",
			"message": "Appliance is rebooting...",
		})
	case "shutdown", "poweroff":
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = s.hwMgr.PowerOff(ctx)
		}()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"action":  "shutdown",
			"message": "Appliance is powering off...",
		})
	default:
		writeHardwareError(w, http.StatusBadRequest, "action must be reboot or shutdown")
	}
}

func (s *Server) getPhotosDir() string {
	photosDir := filepath.Join(filepath.Dir(s.cfg.Database.Path), "photos")
	return photosDir
}

// handleAPIGetPhotos returns available photo slideshow items for the FotoCube ambient screensaver.
func (s *Server) handleAPIGetPhotos(w http.ResponseWriter, r *http.Request) {
	photosDir := s.getPhotosDir()

	photos := make([]string, 0)
	entries, err := os.ReadDir(photosDir)
	if err != nil {
		writeHardwareError(w, http.StatusInternalServerError, "failed to read photo library")
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" {
				photos = append(photos, "/photos/"+url.PathEscape(e.Name()))
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
	r.Body = http.MaxBytesReader(w, r.Body, maxPhotoUploadBytes+(1<<20))
	if err := r.ParseMultipartForm(maxPhotoUploadBytes); err != nil {
		writeHardwareError(w, http.StatusBadRequest, "file too large or invalid multipart form")
		return
	}

	file, header, err := r.FormFile("photo")
	if err != nil {
		writeHardwareError(w, http.StatusBadRequest, "missing 'photo' form field")
		return
	}
	defer file.Close()
	if header.Size <= 0 || header.Size > maxPhotoUploadBytes {
		writeHardwareError(w, http.StatusBadRequest, "photo must be between 1 byte and 32 MiB")
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
		writeHardwareError(w, http.StatusBadRequest, "only JPG, PNG, and WebP images are supported")
		return
	}
	headerBytes := make([]byte, 512)
	n, readErr := file.Read(headerBytes)
	if readErr != nil && readErr != io.EOF {
		writeHardwareError(w, http.StatusBadRequest, "could not read image content")
		return
	}
	contentType := http.DetectContentType(headerBytes[:n])
	if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/webp" {
		writeHardwareError(w, http.StatusBadRequest, "uploaded file content is not a supported image")
		return
	}
	if (contentType == "image/jpeg" && ext != ".jpg" && ext != ".jpeg") ||
		(contentType == "image/png" && ext != ".png") ||
		(contentType == "image/webp" && ext != ".webp") {
		writeHardwareError(w, http.StatusBadRequest, "image content does not match its filename extension")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeHardwareError(w, http.StatusBadRequest, "could not inspect image")
		return
	}
	imageConfig, _, err := image.DecodeConfig(file)
	if err != nil || imageConfig.Width < 1 || imageConfig.Height < 1 ||
		imageConfig.Width > maxPhotoDimension || imageConfig.Height > maxPhotoDimension ||
		int64(imageConfig.Width)*int64(imageConfig.Height) > maxPhotoPixels {
		writeHardwareError(w, http.StatusBadRequest, "image is corrupt or exceeds the 40-megapixel safety limit")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeHardwareError(w, http.StatusBadRequest, "could not prepare image")
		return
	}

	photosDir := s.getPhotosDir()
	if err := os.MkdirAll(photosDir, 0755); err != nil {
		writeHardwareError(w, http.StatusInternalServerError, "failed to prepare photo library")
		return
	}
	entries, err := os.ReadDir(photosDir)
	if err != nil {
		writeHardwareError(w, http.StatusInternalServerError, "failed to inspect photo library")
		return
	}
	var libraryBytes int64
	var photoCount int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr == nil {
			libraryBytes += info.Size()
			photoCount++
		}
	}
	if photoCount >= 2000 || libraryBytes+header.Size > maxPhotoLibraryBytes {
		writeHardwareError(w, http.StatusInsufficientStorage, "photo library quota reached; remove photos before uploading more")
		return
	}
	canonicalExt := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}[contentType]
	randomName, err := auth.GenerateSecureToken(16)
	if err != nil {
		writeHardwareError(w, http.StatusInternalServerError, "failed to allocate a private photo name")
		return
	}
	storedName := randomName + canonicalExt
	dstPath := filepath.Join(photosDir, storedName)

	dst, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		writeHardwareError(w, http.StatusInternalServerError, "failed to save photo")
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, io.LimitReader(file, maxPhotoUploadBytes+1)); err != nil {
		_ = dst.Close()
		_ = os.Remove(dstPath)
		writeHardwareError(w, http.StatusInternalServerError, "failed to write photo content")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = s.store.RecordAudit(r.Context(), &models.AuditEntry{
		ID: models.NewID("aud"), Actor: "household_device", Action: "photo_upload",
		ResourceType: "photo", ResourceID: storedName,
		Details: fmt.Sprintf("Uploaded validated %s photo (%dx%d, %d bytes)", contentType, imageConfig.Width, imageConfig.Height, header.Size),
		IPAddress: remoteIP(r),
	})
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"filename": storedName,
		"url":      "/photos/" + storedName,
	})
}

func sanitizePhotoFilename(name, ext string) string {
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	var safe strings.Builder
	lastWasUnderscore := false
	for _, char := range base {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '_' {
			safe.WriteRune(char)
			lastWasUnderscore = char == '_'
		} else if safe.Len() > 0 && !lastWasUnderscore {
			safe.WriteByte('_')
			lastWasUnderscore = true
		}
		if safe.Len() >= 80 {
			break
		}
	}
	if safe.Len() == 0 {
		safe.WriteString("photo")
	}
	return safe.String() + ext
}

// handleAPIDeletePhoto removes a photo from the FotoCube slideshow directory.
func (s *Server) handleAPIDeletePhoto(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	if filename == "" {
		writeHardwareError(w, http.StatusBadRequest, "filename is required")
		return
	}

	cleanName := filepath.Base(filename)
	if cleanName != filename {
		writeHardwareError(w, http.StatusBadRequest, "invalid filename")
		return
	}
	ext := strings.ToLower(filepath.Ext(cleanName))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
		writeHardwareError(w, http.StatusBadRequest, "invalid photo filename")
		return
	}
	photosDir := s.getPhotosDir()
	targetPath := filepath.Join(photosDir, cleanName)

	if err := os.Remove(targetPath); err != nil {
		if os.IsNotExist(err) {
			writeHardwareError(w, http.StatusNotFound, "photo not found")
			return
		}
		writeHardwareError(w, http.StatusInternalServerError, "failed to delete photo")
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

// handleAPIGetMobilePair returns mobile onboarding pairing info, token, and target URLs.
func (s *Server) handleAPIGetMobilePair(w http.ResponseWriter, r *http.Request) {
	token, err := s.generatePairingToken(r.Context(), 10*time.Minute)
	if err != nil {
		writeHardwareError(w, http.StatusInternalServerError, "failed to create pairing code")
		return
	}
	pairURL := s.buildPairingURL(r, token)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"pair_url":    pairURL,
		"qr_url":      "/api/system/mobile-pair/qrcode",
		"device_name": s.cfg.Server.DeviceName,
		"token":       token,
		"expires_in":  600,
	})
}

// handleAPIGetMobilePairQRCode renders a high-contrast PNG QR code for camera pairing.
func (s *Server) handleAPIGetMobilePairQRCode(w http.ResponseWriter, r *http.Request) {
	token, err := s.generatePairingToken(r.Context(), 10*time.Minute)
	if err != nil {
		writeHardwareError(w, http.StatusInternalServerError, "failed to create pairing code")
		return
	}
	pairURL := s.buildPairingURL(r, token)

	pngBytes, err := qrcode.Encode(pairURL, qrcode.Medium, 280)
	if err != nil {
		writeHardwareError(w, http.StatusInternalServerError, "failed to generate pairing QR code")
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	_, _ = w.Write(pngBytes)
}

func (s *Server) buildPairingURL(r *http.Request, token string) string {
	base, err := url.Parse(s.cfg.Server.BaseURL)
	if err != nil || base.Host == "" {
		base = &url.URL{Scheme: "http", Host: r.Host}
	}
	if s.isLocalhost(r) {
		status := s.hwMgr.Wifi().GetStatus(r.Context())
		if status.IPAddress != "" {
			base.Host = fmt.Sprintf("%s:%d", status.IPAddress, s.cfg.Server.Port)
		} else {
			base.Host = fmt.Sprintf("familyboard.local:%d", s.cfg.Server.Port)
		}
	}
	base.Path = "/"
	base.RawQuery = url.Values{"pair": []string{token}}.Encode()
	base.Fragment = ""
	return base.String()
}
