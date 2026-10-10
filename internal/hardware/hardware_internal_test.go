package hardware

import "testing"

func TestIsSupportedApplianceModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		model string
		want  bool
	}{
		{model: "Raspberry Pi 3 Model B Rev 1.2", want: true},
		{model: "Raspberry Pi 5 Model B Rev 1.0", want: true},
		{model: "Radxa Dragon Q6A\x00", want: true},
		{model: "Qualcomm Technologies, Inc. QCS6490", want: true},
		{model: "Generic x86_64 virtual machine"},
		{model: "developer laptop"},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			t.Parallel()
			if got := isSupportedApplianceModel(tt.model); got != tt.want {
				t.Fatalf("isSupportedApplianceModel(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestParseWifiDevicePrefersConnectedAdapter(t *testing.T) {
	t.Parallel()

	output := "wlp1s0:wifi:disconnected:\nwlx001122334455:wifi:connected:Family\\: Upstairs\neth0:ethernet:connected:Wired\n"
	device, state, connection := parseWifiDevice(output)
	if device != "wlx001122334455" || state != "connected" || connection != "Family: Upstairs" {
		t.Fatalf("unexpected Wi-Fi selection: device=%q state=%q connection=%q", device, state, connection)
	}
}
