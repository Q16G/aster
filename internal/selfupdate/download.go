package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func Apply(ctx context.Context, release *Release, proxy string, progressFn func(phase string, pct int)) error {
	if progressFn == nil {
		progressFn = func(string, int) {}
	}

	archiveName, binaryName := assetNames(release.TagName)
	archiveAsset := findAsset(release.Assets, archiveName)
	if archiveAsset == nil {
		return fmt.Errorf("no asset matching %q in release %s", archiveName, release.TagName)
	}

	checksumAsset := findAsset(release.Assets, "checksums.txt")
	if checksumAsset == nil {
		return fmt.Errorf("checksums.txt not found in release %s", release.TagName)
	}

	client := httpClientWithProxy(&FetchOptions{Proxy: proxy})

	progressFn("downloading checksums", 0)
	expectedHash, err := fetchExpectedChecksum(ctx, client, checksumAsset.BrowserDownloadURL, archiveName)
	if err != nil {
		return fmt.Errorf("fetch checksum: %w", err)
	}

	progressFn("downloading archive", 10)
	archiveData, err := downloadWithChecksum(ctx, client, archiveAsset.BrowserDownloadURL, expectedHash, archiveAsset.Size, func(pct int) {
		progressFn("downloading archive", 10+pct*70/100)
	})
	if err != nil {
		return err
	}

	progressFn("extracting binary", 80)
	newBinary, err := extractBinary(archiveData, archiveName, binaryName)
	if err != nil {
		return fmt.Errorf("extract binary: %w", err)
	}

	progressFn("replacing binary", 90)
	if err := replaceBinary(newBinary); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	progressFn("done", 100)
	return nil
}

func assetNames(tag string) (archive, binary string) {
	return assetNamesFor(tag, runtime.GOOS, runtime.GOARCH)
}

func assetNamesFor(tag, goos, goarch string) (archive, binary string) {
	version := strings.TrimPrefix(tag, "v")
	binary = "aster"

	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
		binary = "aster.exe"
	}

	archive = fmt.Sprintf("aster_%s_%s_%s.%s", version, goos, goarch, ext)
	return archive, binary
}

func findAsset(assets []Asset, name string) *Asset {
	for i := range assets {
		if assets[i].Name == name {
			return &assets[i]
		}
	}
	return nil
}

func fetchExpectedChecksum(ctx context.Context, client *http.Client, url, archiveName string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	addGitHubToken(req)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download checksums returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(body), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == archiveName {
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("checksum for %s not found", archiveName)
}

func downloadWithChecksum(ctx context.Context, client *http.Client, url, expectedHash string, totalSize int64, progressFn func(pct int)) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create download request: %w", err)
	}
	addGitHubToken(req)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download archive: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned %d", resp.StatusCode)
	}

	hasher := sha256.New()
	var buf bytes.Buffer
	reader := io.TeeReader(resp.Body, hasher)

	written := int64(0)
	chunk := make([]byte, 32*1024)
	for {
		n, readErr := reader.Read(chunk)
		if n > 0 {
			buf.Write(chunk[:n])
			written += int64(n)
			if totalSize > 0 {
				progressFn(int(written * 100 / totalSize))
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read archive: %w", readErr)
		}
	}

	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(actualHash, expectedHash) {
		return nil, fmt.Errorf("checksum verification failed: expected %s, got %s", expectedHash, actualHash)
	}

	return buf.Bytes(), nil
}

func extractBinary(archiveData []byte, archiveName, binaryName string) ([]byte, error) {
	if strings.HasSuffix(archiveName, ".zip") {
		return extractFromZip(archiveData, binaryName)
	}
	return extractFromTarGz(archiveData, binaryName)
}

func extractFromTarGz(data []byte, binaryName string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(hdr.Name) == binaryName {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("%s not found in archive", binaryName)
}

func extractFromZip(data []byte, binaryName string) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, f := range r.File {
		if filepath.Base(f.Name) == binaryName {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("%s not found in archive", binaryName)
}

func replaceBinary(newBinary []byte) error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("resolve symlinks: %w", err)
	}

	fi, err := os.Stat(execPath)
	if err != nil {
		return fmt.Errorf("stat current binary: %w", err)
	}

	oldPath := execPath + ".old"
	tmpPath := execPath + ".new"

	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if err := os.WriteFile(tmpPath, newBinary, fi.Mode().Perm()); err != nil {
		return fmt.Errorf("write new binary: %w", err)
	}

	if err := os.Rename(execPath, oldPath); err != nil {
		return fmt.Errorf("backup current binary: %w", err)
	}

	if err := os.Rename(tmpPath, execPath); err != nil {
		if rbErr := os.Rename(oldPath, execPath); rbErr != nil {
			return fmt.Errorf("install new binary failed: %w; rollback also failed: %v; manual recovery: mv %s %s", err, rbErr, oldPath, execPath)
		}
		return fmt.Errorf("install new binary: %w", err)
	}

	_ = os.Remove(oldPath)
	return nil
}

func addGitHubToken(req *http.Request) {
	if token := os.Getenv("ASTER_GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}
