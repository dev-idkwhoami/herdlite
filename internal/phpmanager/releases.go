package phpmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const releasesURL = "https://api.github.com/repos/php/php-src/releases?per_page=100"

type ReleaseClient struct {
	HTTPClient *http.Client
}

type Release struct {
	Version    string
	Minor      string
	Tag        string
	TarballURL string
	HTMLURL    string
	Published  time.Time
	Latest     bool
}

type githubRelease struct {
	TagName     string    `json:"tag_name"`
	TarballURL  string    `json:"tarball_url"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
}

func (c ReleaseClient) Releases(ctx context.Context) ([]Release, error) {
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "herdlite")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github releases request failed: %s", resp.Status)
	}

	var raw []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	releases := make([]Release, 0, len(raw))
	for _, item := range raw {
		if item.Draft || item.Prerelease {
			continue
		}

		version, ok := versionFromTag(item.TagName)
		if !ok {
			continue
		}

		releases = append(releases, Release{
			Version:    version,
			Minor:      minorVersion(version),
			Tag:        item.TagName,
			TarballURL: PHPNetSourceURL(version),
			HTMLURL:    item.HTMLURL,
			Published:  item.PublishedAt,
		})
	}

	sort.Slice(releases, func(i, j int) bool {
		return compareVersion(releases[i].Version, releases[j].Version) > 0
	})
	if len(releases) > 0 {
		releases[0].Latest = true
	}

	return releases, nil
}

func PHPNetSourceURL(version string) string {
	return "https://www.php.net/distributions/php-" + version + ".tar.gz"
}

func ResolveRelease(releases []Release, requested string) (Release, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" || requested == "latest" {
		if len(releases) == 0 {
			return Release{}, fmt.Errorf("no PHP releases found")
		}
		return releases[0], nil
	}

	if isFullVersion(requested) {
		for _, release := range releases {
			if release.Version == requested {
				return release, nil
			}
		}
		return Release{}, fmt.Errorf("PHP %s was not found in release list", requested)
	}

	if isMinorVersion(requested) {
		for _, release := range releases {
			if release.Minor == requested {
				return release, nil
			}
		}
		return Release{}, fmt.Errorf("no PHP release found for minor %s", requested)
	}

	return Release{}, fmt.Errorf("invalid PHP version %q; use latest, 8.5, or 8.5.6", requested)
}

func LatestByMinor(releases []Release) []Release {
	seen := map[string]bool{}
	out := []Release{}
	for _, release := range releases {
		if seen[release.Minor] {
			continue
		}
		seen[release.Minor] = true
		out = append(out, release)
	}
	return out
}

func versionFromTag(tag string) (string, bool) {
	version := strings.TrimPrefix(tag, "php-")
	if !isFullVersion(version) {
		return "", false
	}
	return version, true
}

func minorVersion(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return version
	}
	return parts[0] + "." + parts[1]
}

func isMinorVersion(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 2 {
		return false
	}
	return numeric(parts[0]) && numeric(parts[1])
}

func isFullVersion(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	return numeric(parts[0]) && numeric(parts[1]) && numeric(parts[2])
}

func numeric(value string) bool {
	if value == "" {
		return false
	}
	_, err := strconv.Atoi(value)
	return err == nil
}

func compareVersion(a string, b string) int {
	ap := parseVersion(a)
	bp := parseVersion(b)
	for i := 0; i < len(ap); i++ {
		if ap[i] > bp[i] {
			return 1
		}
		if ap[i] < bp[i] {
			return -1
		}
	}
	return 0
}

func parseVersion(version string) [3]int {
	var out [3]int
	parts := strings.Split(version, ".")
	for i := 0; i < len(parts) && i < len(out); i++ {
		value, _ := strconv.Atoi(parts[i])
		out[i] = value
	}
	return out
}
