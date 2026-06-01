package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"
)

const (
	releasesURL    = "https://api.github.com/repos/Q16G/aster/releases"
	releaseByTagFn = "https://api.github.com/repos/Q16G/aster/releases/tags/%s"
)

// DefaultChannels is the set of release channels that `aster update` adopts by
// default. Alpha (and unknown) prereleases are excluded and can only be
// installed by explicitly passing --version.
var DefaultChannels = map[string]bool{"stable": true, "beta": true}

type Release struct {
	TagName    string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type FetchOptions struct {
	Proxy string
}

// FetchReleases lists all releases (including prereleases) from GitHub.
func FetchReleases(ctx context.Context, opts *FetchOptions) ([]Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var releases []Release
	if err := getJSON(ctx, releasesURL+"?per_page=100", opts, &releases); err != nil {
		return nil, err
	}
	return releases, nil
}

// FetchReleaseByTag fetches a single release by its exact tag name.
func FetchReleaseByTag(ctx context.Context, tag string, opts *FetchOptions) (*Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var rel Release
	if err := getJSON(ctx, fmt.Sprintf(releaseByTagFn, url.PathEscape(tag)), opts, &rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// SelectLatest returns the highest-version release whose channel is in the
// given set, skipping drafts and unparseable tags. Returns nil when none match.
func SelectLatest(releases []Release, channels map[string]bool) *Release {
	var best *Release
	var bestVer Version
	for i := range releases {
		rel := &releases[i]
		if rel.Draft {
			continue
		}
		ver, err := ParseVersion(rel.TagName)
		if err != nil {
			continue
		}
		if !channels[ver.Channel()] {
			continue
		}
		if best == nil || Compare(ver, bestVer) > 0 {
			best = rel
			bestVer = ver
		}
	}
	return best
}

func getJSON(ctx context.Context, endpoint string, opts *FetchOptions, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	if token := os.Getenv("ASTER_GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := httpClientWithProxy(opts)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github api returned %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode release: %w", err)
	}
	return nil
}
