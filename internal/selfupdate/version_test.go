package selfupdate

import "testing"

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input               string
		major, minor, patch int
		pre                 []string
		wantErr             bool
	}{
		{"v1.2.3", 1, 2, 3, nil, false},
		{"v0.0.1", 0, 0, 1, nil, false},
		{"1.2.3", 1, 2, 3, nil, false},
		{"v10.20.30", 10, 20, 30, nil, false},
		{"v1.1.0-beta", 1, 1, 0, []string{"beta"}, false},
		{"v1.1.0-alpha-2", 1, 1, 0, []string{"alpha", "2"}, false},
		{"", 0, 0, 0, nil, true},
		{"v1.2", 0, 0, 0, nil, true},
		{"v1.2.x", 0, 0, 0, nil, true},
		{"v1.2.3-", 0, 0, 0, nil, true},
		{"dev", 0, 0, 0, nil, true},
	}
	for _, tt := range tests {
		v, err := ParseVersion(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseVersion(%q) err=%v, wantErr=%v", tt.input, err, tt.wantErr)
			continue
		}
		if err != nil {
			continue
		}
		if v.Major != tt.major || v.Minor != tt.minor || v.Patch != tt.patch {
			t.Errorf("ParseVersion(%q) = %d.%d.%d, want %d.%d.%d", tt.input, v.Major, v.Minor, v.Patch, tt.major, tt.minor, tt.patch)
		}
		if !equalSlice(v.Pre, tt.pre) {
			t.Errorf("ParseVersion(%q) pre = %v, want %v", tt.input, v.Pre, tt.pre)
		}
	}
}

func TestChannel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"v1.2.3", "stable"},
		{"v1.0.0-beta", "beta"},
		{"v1.0.0-beta-1", "beta"},
		{"v1.1.0-alpha-2", "alpha"},
		{"v1.1.0-rc-1", "unknown"},
	}
	for _, tt := range tests {
		v, err := ParseVersion(tt.input)
		if err != nil {
			t.Fatalf("ParseVersion(%q) unexpected err: %v", tt.input, err)
		}
		if got := v.Channel(); got != tt.want {
			t.Errorf("Channel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"v1.0.0", "v1.0.0", 0},
		{"v1.1.0-beta", "v1.1.0-beta", 0},
		{"v1.0.0", "v1.0.1", -1},
		{"v1.0.1", "v1.1.0", -1},
		{"v1.1.0", "v2.0.0", -1},
		// no suffix outranks any prerelease at the same MMP
		{"v1.1.0", "v1.1.0-beta", 1},
		// channel ordering: alpha < beta
		{"v1.1.0-alpha-2", "v1.1.0-beta", -1},
		// same channel, numeric ordering
		{"v1.1.0-alpha-1", "v1.1.0-alpha-2", -1},
		// fewer leading-equal identifiers ranks lower
		{"v1.1.0-beta", "v1.1.0-beta-1", -1},
	}
	for _, tt := range tests {
		av, err := ParseVersion(tt.a)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", tt.a, err)
		}
		bv, err := ParseVersion(tt.b)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", tt.b, err)
		}
		if got := Compare(av, bv); got != tt.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
		// antisymmetry: Compare(a,b) == -Compare(b,a)
		if got, rev := Compare(av, bv), Compare(bv, av); got != -rev {
			t.Errorf("Compare antisymmetry violated for (%q, %q): %d vs %d", tt.a, tt.b, got, rev)
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
		// prerelease ordering: alpha < beta < stable
		{"v1.1.0-alpha-1", "v1.1.0-alpha-2", true},
		{"v1.1.0-alpha-2", "v1.1.0-beta", true},
		{"v1.1.0-beta", "v1.1.0", true},
		{"v1.1.0-beta", "v1.1.0-alpha-2", false},
		{"v1.0.0-beta", "v1.1.0-alpha-1", true},
		{"v1.0.0-beta", "v1.0.0-beta", false},
	}
	for _, tt := range tests {
		got := IsNewer(tt.current, tt.latest)
		if got != tt.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
		}
	}
}

func TestSelectLatest(t *testing.T) {
	releases := []Release{
		{TagName: "v1.1.0-alpha-2"},
		{TagName: "v1.1.0-alpha-1"},
		{TagName: "v1.0.0-beta"},
		{TagName: "v0.9.0", Draft: true},
		{TagName: "not-a-version"},
	}

	// Default channels exclude alpha, so the newest eligible is the beta.
	rel := SelectLatest(releases, DefaultChannels)
	if rel == nil || rel.TagName != "v1.0.0-beta" {
		t.Fatalf("SelectLatest(default) = %v, want v1.0.0-beta", rel)
	}

	// Drafts are skipped even when their channel is allowed.
	stableOnly := SelectLatest(releases, map[string]bool{"stable": true})
	if stableOnly != nil {
		t.Fatalf("SelectLatest(stable) = %v, want nil (only stable is a draft)", stableOnly)
	}

	// When alpha is allowed, the newest alpha wins over the beta.
	withAlpha := SelectLatest(releases, map[string]bool{"alpha": true, "beta": true})
	if withAlpha == nil || withAlpha.TagName != "v1.1.0-alpha-2" {
		t.Fatalf("SelectLatest(alpha+beta) = %v, want v1.1.0-alpha-2", withAlpha)
	}
}

func TestSelectLatest_Boundaries(t *testing.T) {
	// Empty slice -> nil.
	if rel := SelectLatest(nil, DefaultChannels); rel != nil {
		t.Fatalf("SelectLatest(nil) = %v, want nil", rel)
	}

	// All alpha + default channels -> nil (alpha excluded).
	allAlpha := []Release{{TagName: "v1.1.0-alpha-2"}, {TagName: "v1.0.0-alpha-1"}}
	if rel := SelectLatest(allAlpha, DefaultChannels); rel != nil {
		t.Fatalf("SelectLatest(all alpha, default) = %v, want nil", rel)
	}

	// Stable and beta both present (non-draft) -> stable wins at higher/equal MMP.
	stableVsBeta := []Release{{TagName: "v1.0.0-beta"}, {TagName: "v1.0.0"}}
	if rel := SelectLatest(stableVsBeta, DefaultChannels); rel == nil || rel.TagName != "v1.0.0" {
		t.Fatalf("SelectLatest(stable vs beta) = %v, want v1.0.0", rel)
	}

	// Unknown suffix is excluded by default channels.
	withUnknown := []Release{{TagName: "v1.2.0-rc-1"}, {TagName: "v1.0.0-beta"}}
	if rel := SelectLatest(withUnknown, DefaultChannels); rel == nil || rel.TagName != "v1.0.0-beta" {
		t.Fatalf("SelectLatest(unknown excluded) = %v, want v1.0.0-beta", rel)
	}

	// Highest version is a draft -> skip it, pick the next non-draft.
	highestDraft := []Release{{TagName: "v2.0.0", Draft: true}, {TagName: "v1.0.0-beta"}}
	if rel := SelectLatest(highestDraft, DefaultChannels); rel == nil || rel.TagName != "v1.0.0-beta" {
		t.Fatalf("SelectLatest(highest draft) = %v, want v1.0.0-beta", rel)
	}
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
