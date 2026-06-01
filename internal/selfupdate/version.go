package selfupdate

import (
	"fmt"
	"strconv"
	"strings"
)

// Version represents a parsed semantic version with an optional prerelease
// suffix. The prerelease separator in this project is "-" (e.g. v1.1.0-alpha-2),
// so Pre holds the dash-separated identifiers after the MAJOR.MINOR.PATCH core.
type Version struct {
	Major, Minor, Patch int
	Pre                 []string
	Raw                 string
}

// Channel classifies a version into a release channel based on its prerelease
// suffix: no suffix -> "stable", -beta... -> "beta", -alpha... -> "alpha",
// anything else -> "unknown" (treated as alpha-level, excluded by default).
func (v Version) Channel() string {
	if len(v.Pre) == 0 {
		return "stable"
	}
	switch strings.ToLower(v.Pre[0]) {
	case "beta":
		return "beta"
	case "alpha":
		return "alpha"
	default:
		return "unknown"
	}
}

func ParseVersion(s string) (Version, error) {
	raw := s
	s = strings.TrimPrefix(s, "v")

	core := s
	var pre []string
	if i := strings.Index(s, "-"); i >= 0 {
		core = s[:i]
		preStr := s[i+1:]
		if preStr == "" {
			return Version{}, fmt.Errorf("invalid version %q: empty prerelease", raw)
		}
		pre = strings.Split(preStr, "-")
	}

	parts := strings.SplitN(core, ".", 3)
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("invalid version %q: expected vMAJOR.MINOR.PATCH", raw)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return Version{}, fmt.Errorf("invalid major %q: %w", parts[0], err)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return Version{}, fmt.Errorf("invalid minor %q: %w", parts[1], err)
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return Version{}, fmt.Errorf("invalid patch %q: %w", parts[2], err)
	}
	return Version{Major: major, Minor: minor, Patch: patch, Pre: pre, Raw: raw}, nil
}

// Compare returns -1, 0 or 1 reporting whether a is less than, equal to or
// greater than b. Ordering follows semver precedence: MAJOR.MINOR.PATCH first,
// then a version without a prerelease ranks higher than one with, and otherwise
// prerelease identifiers are compared one by one (numeric identifiers compare
// numerically and rank lower than alphanumeric ones; a shorter set of leading-
// equal identifiers ranks lower).
func Compare(a, b Version) int {
	if c := cmpInt(a.Major, b.Major); c != 0 {
		return c
	}
	if c := cmpInt(a.Minor, b.Minor); c != 0 {
		return c
	}
	if c := cmpInt(a.Patch, b.Patch); c != 0 {
		return c
	}

	aStable := len(a.Pre) == 0
	bStable := len(b.Pre) == 0
	if aStable && bStable {
		return 0
	}
	if aStable {
		return 1
	}
	if bStable {
		return -1
	}
	return comparePre(a.Pre, b.Pre)
}

func comparePre(a, b []string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if c := comparePreID(a[i], b[i]); c != 0 {
			return c
		}
	}
	return cmpInt(len(a), len(b))
}

func comparePreID(a, b string) int {
	aNum, aErr := strconv.Atoi(a)
	bNum, bErr := strconv.Atoi(b)
	aIsNum := aErr == nil
	bIsNum := bErr == nil
	if aIsNum && bIsNum {
		return cmpInt(aNum, bNum)
	}
	// Numeric identifiers always have lower precedence than alphanumeric ones.
	if aIsNum {
		return -1
	}
	if bIsNum {
		return 1
	}
	return strings.Compare(a, b)
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func IsNewer(current, latest string) bool {
	if strings.EqualFold(current, "dev") {
		return true
	}
	cur, err := ParseVersion(current)
	if err != nil {
		return false
	}
	lat, err := ParseVersion(latest)
	if err != nil {
		return false
	}
	return Compare(lat, cur) > 0
}
