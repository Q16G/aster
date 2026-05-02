package builtin_tools

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const embeddedRipgrepVersion = "15.1.0"

//go:embed vendor/ripgrep/*/*
var embeddedRipgrepFS embed.FS

type ripgrepConfig struct {
	Mode    string
	Command string
	Args    []string
}

var (
	ripgrepExtractMu    sync.Mutex
	ripgrepExtractedBin = make(map[string]string)
)

func RipgrepConfig() (*ripgrepConfig, error) {
	attempts := make([]string, 0, 2)
	if isEnvDefinedFalsy(os.Getenv("USE_BUILTIN_RIPGREP")) {
		if _, err := exec.LookPath("rg"); err == nil {
			return &ripgrepConfig{
				Mode:    "system",
				Command: "rg",
			}, nil
		} else {
			attempts = append(attempts, fmt.Sprintf("system rg lookup failed: %v", err))
		}
	} else {
		attempts = append(attempts, "system rg skipped: USE_BUILTIN_RIPGREP not explicitly false")
	}

	command, err := ensureEmbeddedRipgrepBinary()
	if err == nil {
		return &ripgrepConfig{
			Mode:    "embedded",
			Command: command,
		}, nil
	}
	attempts = append(attempts, fmt.Sprintf("embedded ripgrep failed: %v", err))
	return nil, fmt.Errorf("rg executable unavailable (%s)", strings.Join(attempts, "; "))
}

func ensureEmbeddedRipgrepBinary() (string, error) {
	assetRel, binName, err := embeddedRipgrepAsset(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}
	cacheKey := runtime.GOOS + "/" + runtime.GOARCH

	ripgrepExtractMu.Lock()
	defer ripgrepExtractMu.Unlock()

	if path := strings.TrimSpace(ripgrepExtractedBin[cacheKey]); path != "" {
		if info, statErr := os.Stat(path); statErr == nil && info.Mode().IsRegular() {
			return path, nil
		}
		delete(ripgrepExtractedBin, cacheKey)
	}

	payload, err := fs.ReadFile(embeddedRipgrepFS, filepath.ToSlash(assetRel))
	if err != nil {
		return "", fmt.Errorf("read embedded asset %s: %w", assetRel, err)
	}

	targetDir := filepath.Join(os.TempDir(), "sastpro-ripgrep", embeddedRipgrepVersion, strings.ReplaceAll(cacheKey, "/", "-"))
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", fmt.Errorf("create ripgrep temp dir: %w", err)
	}
	targetPath := filepath.Join(targetDir, binName)
	if err := os.WriteFile(targetPath, payload, 0o755); err != nil {
		return "", fmt.Errorf("write embedded ripgrep binary: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(targetPath, 0o755); err != nil {
			return "", fmt.Errorf("chmod embedded ripgrep binary: %w", err)
		}
	}
	ripgrepExtractedBin[cacheKey] = targetPath
	return targetPath, nil
}

func embeddedRipgrepAsset(goos string, goarch string) (assetRel string, binName string, err error) {
	switch {
	case goos == "darwin" && goarch == "arm64":
		return "vendor/ripgrep/arm64-darwin/rg", "rg", nil
	case goos == "darwin" && (goarch == "amd64" || goarch == "x86_64"):
		return "vendor/ripgrep/amd64-darwin/rg", "rg", nil
	case goos == "linux" && goarch == "arm64":
		return "vendor/ripgrep/arm64-linux/rg", "rg", nil
	case goos == "linux" && (goarch == "amd64" || goarch == "x86_64"):
		return "vendor/ripgrep/amd64-linux/rg", "rg", nil
	case goos == "windows" && (goarch == "amd64" || goarch == "x86_64"):
		return "vendor/ripgrep/amd64-windows/rg.exe", "rg.exe", nil
	default:
		return "", "", fmt.Errorf("unsupported platform for embedded ripgrep: %s/%s", goos, goarch)
	}
}

func isEnvDefinedFalsy(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return false
	}
	switch value {
	case "0", "false", "no", "off":
		return true
	default:
		return false
	}
}
