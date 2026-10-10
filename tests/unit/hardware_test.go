package unit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/hardware"
	"github.com/Hardonian/FamilyBoard/internal/web"
)

func TestHardware_ManagerDirect(t *testing.T) {
	mgr := hardware.NewManager()
	ctx := context.Background()

	// 1. System Info
	info := mgr.GetSystemInfo(ctx)
	if info.Architecture == "" {
		t.Errorf("expected non-empty architecture")
	}
	if info.NumCPU <= 0 {
		t.Errorf("expected NumCPU > 0, got %d", info.NumCPU)
	}

	// 2. Wi-Fi
	wifi := mgr.Wifi()
	status := wifi.GetStatus(ctx)
	if status.Interface == "" {
		t.Errorf("expected non-empty interface")
	}

	aps, err := wifi.ScanNetworks(ctx)
	if err != nil {
		t.Fatalf("scan networks error: %v", err)
	}
	if len(aps) == 0 {
		t.Errorf("expected at least 1 mock or discovered AP")
	}

	if err := wifi.Connect(ctx, "TestHomeNetwork", "secret123"); err != nil {
		t.Errorf("wifi connect failed: %v", err)
	}

	if err := wifi.ToggleHotspot(ctx, true); err != nil {
		t.Errorf("toggle hotspot true failed: %v", err)
	}
	if err := wifi.ToggleHotspot(ctx, false); err != nil {
		t.Errorf("toggle hotspot false failed: %v", err)
	}

	// 3. Bluetooth & Beacons
	bt := mgr.Bluetooth()
	btStatus := bt.GetStatus(ctx)
	if !btStatus.Powered {
		t.Errorf("expected mock bluetooth powered to be true")
	}

	if err := bt.SetPower(ctx, false); err != nil {
		t.Errorf("set power false failed: %v", err)
	}
	_ = bt.SetPower(ctx, true)

	devices, err := bt.ScanDevices(ctx)
	if err != nil {
		t.Fatalf("scan devices error: %v", err)
	}
	if len(devices) == 0 {
		t.Errorf("expected discovered bluetooth devices")
	}

	beacons := bt.ListBeacons()
	if len(beacons) == 0 {
		t.Errorf("expected default beacons")
	}

	// Register new beacon
	customBeacon := hardware.FamilyBeacon{
		ID:         "test-beacon-custom",
		PersonName: "Sam",
		DeviceName: "Sam's Smartwatch",
		MACAddress: "AA:BB:CC:DD:EE:FF",
		LastSeen:   time.Now(),
		RSSI:       -55,
	}
	bt.RegisterBeacon(customBeacon)

	updatedBeacons := bt.ListBeacons()
	found := false
	for _, b := range updatedBeacons {
		if b.ID == "test-beacon-custom" {
			found = true
			if b.IsPresent {
				t.Errorf("newly registered beacon must remain away until actually observed")
			}
			break
		}
	}
	if !found {
		t.Errorf("expected custom beacon to be found")
	}

	// Remove beacon
	bt.RemoveBeacon("test-beacon-custom")
	finalBeacons := bt.ListBeacons()
	for _, b := range finalBeacons {
		if b.ID == "test-beacon-custom" {
			t.Errorf("beacon should have been removed")
		}
	}
}

func TestHardware_HTTPEndpoints(t *testing.T) {
	srv, store, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. GET /api/system/hardware
	{
		req := httptest.NewRequest(http.MethodGet, "/api/system/hardware", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for hardware info, got %d", w.Code)
		}
		var info hardware.SystemInfo
		if err := json.NewDecoder(w.Body).Decode(&info); err != nil {
			t.Fatalf("decode hardware info: %v", err)
		}
		if info.HardwareModel == "" {
			t.Errorf("expected non-empty hardware model")
		}
	}

	// 2. GET /api/system/wifi
	{
		req := httptest.NewRequest(http.MethodGet, "/api/system/wifi", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for wifi status, got %d", w.Code)
		}
	}

	// 3. GET /api/system/wifi/scan
	{
		req := httptest.NewRequest(http.MethodGet, "/api/system/wifi/scan", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for wifi scan, got %d", w.Code)
		}
	}

	// 4. POST /api/system/wifi/connect (localhost physical kiosk access)
	{
		payload := []byte(`{"ssid":"RadxaHome5G","password":"mypassword123"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/system/wifi/connect", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for wifi connect, got %d (body: %s)", w.Code, w.Body.String())
		}
	}

	// 5. POST /api/system/wifi/hotspot
	{
		payload := []byte(`{"enable":true}`)
		req := httptest.NewRequest(http.MethodPost, "/api/system/wifi/hotspot", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for hotspot toggle, got %d", w.Code)
		}
	}

	// 6. GET /api/system/bluetooth
	{
		req := httptest.NewRequest(http.MethodGet, "/api/system/bluetooth", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for bluetooth status, got %d", w.Code)
		}
	}

	// 7. POST /api/system/bluetooth/power
	{
		payload := []byte(`{"powered":true}`)
		req := httptest.NewRequest(http.MethodPost, "/api/system/bluetooth/power", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for bluetooth power, got %d", w.Code)
		}
	}

	// 8. GET /api/system/presence
	{
		req := httptest.NewRequest(http.MethodGet, "/api/system/presence", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for presence, got %d", w.Code)
		}
		var beacons []hardware.FamilyBeacon
		if err := json.NewDecoder(w.Body).Decode(&beacons); err != nil {
			t.Fatalf("decode beacons: %v", err)
		}
		if len(beacons) == 0 {
			t.Errorf("expected default beacons")
		}
	}

	// 9. POST /api/system/presence/beacon
	{
		payload := []byte(`{"person_name":"Taylor","device_name":"Taylor Phone","mac_address":"11:22:33:44:55:66"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/system/presence/beacon", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for register beacon, got %d (body: %s)", w.Code, w.Body.String())
		}
		persisted, err := store.GetSetting(context.Background(), "presence_beacons")
		if err != nil || !strings.Contains(persisted, "Taylor") {
			t.Errorf("expected registered beacon to be persisted, got %q (err: %v)", persisted, err)
		}
	}

	// 10. POST /api/system/power (requires admin auth)
	{
		cookie, csrfCookie, csrfToken := loginAdminForTest(t, srv)
		payload := []byte(`{"action":"reboot"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/system/power", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		req.AddCookie(csrfCookie)
		req.Header.Set("X-CSRF-Token", csrfToken)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for system power, got %d (body: %s)", w.Code, w.Body.String())
		}
	}

	// 11. GET /api/photos (FotoCube Ambient Photo Frame) & Upload/Delete
	{
		req := httptest.NewRequest(http.MethodGet, "/api/photos", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for /api/photos, got %d", w.Code)
		}

		// Upload photo test
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("photo", "family_trip.jpg")
		if err != nil {
			t.Fatalf("failed to create form file: %v", err)
		}
		_, _ = part.Write([]byte("\xFF\xD8\xFF\xE0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00\xFF\xDB"))
		_ = writer.Close()

		cookie, _, _ := loginAdminForTest(t, srv)
		uploadReq := httptest.NewRequest(http.MethodPost, "/api/photos/upload", &body)
		uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
		uploadReq.AddCookie(cookie)
		uploadW := httptest.NewRecorder()
		srv.ServeHTTP(uploadW, uploadReq)
		if uploadW.Code != http.StatusOK {
			t.Fatalf("expected 200 for photo upload, got %d: %s", uploadW.Code, uploadW.Body.String())
		}

		var uploadResp map[string]interface{}
		_ = json.Unmarshal(uploadW.Body.Bytes(), &uploadResp)
		uploadedFilename, ok := uploadResp["filename"].(string)
		if !ok || uploadedFilename == "" {
			t.Fatalf("expected uploaded filename in response")
		}

		// Verify listed
		listReq := httptest.NewRequest(http.MethodGet, "/api/photos", nil)
		listReq.RemoteAddr = "127.0.0.1:1234"
		listW := httptest.NewRecorder()
		srv.ServeHTTP(listW, listReq)
		if listW.Code != http.StatusOK {
			t.Fatalf("expected 200 for /api/photos, got %d", listW.Code)
		}
		if !bytes.Contains(listW.Body.Bytes(), []byte(uploadedFilename)) {
			t.Errorf("expected photo list to contain %s", uploadedFilename)
		}

		// Delete photo
		deleteReq := httptest.NewRequest(http.MethodDelete, "/api/photos/"+uploadedFilename, nil)
		deleteReq.AddCookie(cookie)
		deleteW := httptest.NewRecorder()
		srv.ServeHTTP(deleteW, deleteReq)
		if deleteW.Code != http.StatusOK {
			t.Fatalf("expected 200 for photo delete, got %d: %s", deleteW.Code, deleteW.Body.String())
		}
	}

	// 11b. Wi-Fi QR onboarding is available to the physical kiosk but not remote unauthenticated clients.
	{
		localReq := httptest.NewRequest(http.MethodGet, "/api/system/wifi/qrcode", nil)
		localReq.RemoteAddr = "127.0.0.1:1234"
		localW := httptest.NewRecorder()
		srv.ServeHTTP(localW, localReq)
		if localW.Code != http.StatusOK {
			t.Errorf("expected 200 for local Wi-Fi QR code, got %d", localW.Code)
		}
		if got := localW.Header().Get("Content-Type"); got != "image/png" {
			t.Errorf("expected image/png QR response, got %q", got)
		}

		remoteReq := httptest.NewRequest(http.MethodGet, "/api/system/wifi/qrcode", nil)
		remoteReq.RemoteAddr = "192.0.2.25:1234"
		remoteW := httptest.NewRecorder()
		srv.ServeHTTP(remoteW, remoteReq)
		if remoteW.Code != http.StatusUnauthorized {
			t.Errorf("expected remote Wi-Fi QR request to require authorization, got %d", remoteW.Code)
		}
	}

	// 12. POST /api/system/storage/migrate-nvme
	{
		cookie, csrfCookie, csrfToken := loginAdminForTest(t, srv)
		req := httptest.NewRequest(http.MethodPost, "/api/system/storage/migrate-nvme", nil)
		req.AddCookie(cookie)
		req.AddCookie(csrfCookie)
		req.Header.Set("X-CSRF-Token", csrfToken)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for migrate-nvme, got %d", w.Code)
		}
	}

	// 13. Admin Dashboard with Hardware Tab
	{
		req := httptest.NewRequest(http.MethodGet, "/admin?tab=hardware", nil)
		cookie, _, _ := loginAdminForTest(t, srv)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for /admin?tab=hardware, got %d", w.Code)
		}
		body := w.Body.String()
		if !bytes.Contains([]byte(body), []byte("Radxa Dragon")) {
			t.Errorf("expected admin page to contain 'Radxa Dragon'")
		}
		if !bytes.Contains([]byte(body), []byte("Wi-Fi 6E")) {
			t.Errorf("expected admin page to contain 'Wi-Fi 6E'")
		}
		if !bytes.Contains([]byte(body), []byte("Bluetooth 5.2")) {
			t.Errorf("expected admin page to contain 'Bluetooth 5.2'")
		}
		if !bytes.Contains([]byte(body), []byte("NVMe")) {
			t.Errorf("expected admin page to contain 'NVMe'")
		}
	}
}

func TestHardware_RemotePrivacyAndInputValidation(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	for _, path := range []string{
		"/api/system/hardware",
		"/api/system/wifi",
		"/api/system/wifi/scan",
		"/api/system/bluetooth",
		"/api/system/presence",
		"/api/photos",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "192.0.2.44:54321"
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected remote unauthenticated %s to return 401, got %d", path, w.Code)
		}
	}

	invalidRequests := []struct {
		path string
		body string
	}{
		{"/api/system/wifi/connect", `{"ssid":"Home","password":"short"}`},
		{"/api/system/wifi/connect", `{"ssid":"Home","password":"validpass","unexpected":true}`},
		{"/api/system/presence/beacon", `{"person_name":"Taylor","device_name":"Phone","mac_address":"not-a-mac"}`},
	}
	for _, tc := range invalidRequests {
		req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected invalid request to %s to return 400, got %d: %s", tc.path, w.Code, w.Body.String())
		}
		if w.Header().Get("Content-Type") != "application/json" {
			t.Errorf("expected JSON error content type for %s", tc.path)
		}
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("photo", "not-really.jpg")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("plain text masquerading as an image"))
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/photos/upload", &body)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected disguised image upload to be rejected, got %d: %s", w.Code, w.Body.String())
	}
}

func loginAdminForTest(t *testing.T, srv *web.Server) (*http.Cookie, *http.Cookie, string) {
	t.Helper()
	csrfCookie, csrfToken := csrfForTest(t, srv, "/login")
	form := "username=admin&password=correct-horse-battery&csrf_token=" + url.QueryEscape(csrfToken)
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	for _, c := range w.Result().Cookies() {
		if c.Name == "fb_session" {
			return c, csrfCookie, csrfToken
		}
	}
	t.Fatalf("expected admin session cookie")
	return nil, nil, ""
}
