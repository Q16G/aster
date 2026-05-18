package selfupdate

import "testing"

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input               string
		major, minor, patch int
		wantErr             bool
	}{
		{"v1.2.3", 1, 2, 3, false},
		{"v0.0.1", 0, 0, 1, false},
		{"1.2.3", 1, 2, 3, false},
		{"v10.20.30", 10, 20, 30, false},
		{"", 0, 0, 0, true},
		{"v1.2", 0, 0, 0, true},
		{"v1.2.x", 0, 0, 0, true},
		{"dev", 0, 0, 0, true},
	}
	for _, tt := range tests {
		maj, min, pat, err := ParseVersion(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseVersion(%q) err=%v, wantErr=%v", tt.input, err, tt.wantErr)
			continue
		}
		if err == nil && (maj != tt.major || min != tt.minor || pat != tt.patch) {
			t.Errorf("ParseVersion(%q) = %d.%d.%d, want %d.%d.%d", tt.input, maj, min, pat, tt.major, tt.minor, tt.patch)
		}
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"v1.0.0", "v1.0.1", true},
		{"v1.0.0", "v1.1.0", true},
		{"v1.0.0", "v2.0.0", true},
		{"v1.0.1", "v1.0.0", false},
		{"v1.0.0", "v1.0.0", false},
		{"dev", "v0.0.1", true},
		{"dev", "v999.0.0", true},
		{"invalid", "v1.0.0", false},
		{"v1.0.0", "invalid", false},
	}
	for _, tt := range tests {
		got := IsNewer(tt.current, tt.latest)
		if got != tt.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
		}
	}
}
