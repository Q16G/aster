package selfupdate

import (
	"fmt"
	"strconv"
	"strings"
)

func ParseVersion(s string) (major, minor, patch int, err error) {
	s = strings.TrimPrefix(s, "v")
	parts := strings.SplitN(s, ".", 3)
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("invalid version %q: expected vMAJOR.MINOR.PATCH", s)
	}
	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid major %q: %w", parts[0], err)
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid minor %q: %w", parts[1], err)
	}
	patch, err = strconv.Atoi(parts[2])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid patch %q: %w", parts[2], err)
	}
	return major, minor, patch, nil
}

func IsNewer(current, latest string) bool {
	if strings.EqualFold(current, "dev") {
		return true
	}
	cMaj, cMin, cPat, err := ParseVersion(current)
	if err != nil {
		return false
	}
	lMaj, lMin, lPat, err := ParseVersion(latest)
	if err != nil {
		return false
	}
	if lMaj != cMaj {
		return lMaj > cMaj
	}
	if lMin != cMin {
		return lMin > cMin
	}
	return lPat > cPat
}
