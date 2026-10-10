package hardware

import (
	"reflect"
	"testing"
)

func TestSplitNMTerseLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []string
	}{
		{
			name: "ordinary access point",
			line: "*:FamilyBoard:92:WPA2",
			want: []string{"*", "FamilyBoard", "92", "WPA2"},
		},
		{
			name: "escaped colon in ssid",
			line: `:Family\: Upstairs:74:WPA3`,
			want: []string{"", "Family: Upstairs", "74", "WPA3"},
		},
		{
			name: "escaped backslash",
			line: `:Family\\Home:65:WPA2`,
			want: []string{"", `Family\Home`, "65", "WPA2"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := splitNMTerseLine(test.line); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("splitNMTerseLine(%q) = %#v, want %#v", test.line, got, test.want)
			}
		})
	}
}
