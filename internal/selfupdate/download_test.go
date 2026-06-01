package selfupdate

import (
	"runtime"
	"testing"
)

func TestAssetNames(t *testing.T) {
	tests := []struct {
		tag         string
		goos        string
		goarch      string
		wantArchive string
		wantBinary  string
	}{
		{"v1.0.0-beta", "darwin", "arm64", "aster_1.0.0-beta_darwin_arm64.tar.gz", "aster"},
		{"v1.1.0-alpha-2", "linux", "amd64", "aster_1.1.0-alpha-2_linux_amd64.tar.gz", "aster"},
		{"v2.0.0", "windows", "amd64", "aster_2.0.0_windows_amd64.zip", "aster.exe"},
		{"v1.1.0-alpha-2", "windows", "arm64", "aster_1.1.0-alpha-2_windows_arm64.zip", "aster.exe"},
		{"v2.0.0", "linux", "arm64", "aster_2.0.0_linux_arm64.tar.gz", "aster"},
	}
	for _, tt := range tests {
		archive, binary := assetNamesFor(tt.tag, tt.goos, tt.goarch)
		if archive != tt.wantArchive {
			t.Errorf("assetNamesFor(%q, %q, %q) archive = %q, want %q", tt.tag, tt.goos, tt.goarch, archive, tt.wantArchive)
		}
		if binary != tt.wantBinary {
			t.Errorf("assetNamesFor(%q, %q, %q) binary = %q, want %q", tt.tag, tt.goos, tt.goarch, binary, tt.wantBinary)
		}
	}
}

func TestAssetNames_CurrentPlatform(t *testing.T) {
	const tag = "v1.1.0-alpha-2"
	gotArchive, gotBinary := assetNames(tag)
	wantArchive, wantBinary := assetNamesFor(tag, runtime.GOOS, runtime.GOARCH)
	if gotArchive != wantArchive || gotBinary != wantBinary {
		t.Errorf("assetNames(%q) = (%q, %q), want (%q, %q)", tag, gotArchive, gotBinary, wantArchive, wantBinary)
	}
}
