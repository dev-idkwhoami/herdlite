package phpmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"herdlite/internal/state"
)

const eolURL = "https://www.php.net/releases/?json"

type EOLClient struct {
	HTTPClient *http.Client
}

type EOLBranch struct {
	Minor       string
	EOLDate     string
	LastRelease string
}

func (c EOLClient) Branches(ctx context.Context) ([]EOLBranch, error) {
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, eolURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "herdlite")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("php.net EOL request failed: %s", resp.Status)
	}

	return ParseEOL(resp.Body)
}

func SyncEOL(ctx context.Context, store *state.Store) ([]state.PHPEOLBranch, error) {
	branches, err := (EOLClient{}).Branches(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	stateBranches := make([]state.PHPEOLBranch, 0, len(branches))
	for _, branch := range branches {
		stateBranches = append(stateBranches, state.PHPEOLBranch{
			Minor:       branch.Minor,
			EOLDate:     branch.EOLDate,
			LastRelease: branch.LastRelease,
			FetchedAt:   now,
		})
	}

	if err := store.ReplacePHPEOLBranches(stateBranches, now); err != nil {
		return nil, err
	}

	return stateBranches, nil
}

func ParseEOL(r io.Reader) ([]EOLBranch, error) {
	var raw map[string]phpNetRelease
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, err
	}

	supported := map[string]bool{}
	for _, release := range raw {
		for _, minor := range release.SupportedVersions {
			supported[minor] = true
		}
	}

	branches := []EOLBranch{}
	for _, release := range raw {
		if !isFullVersion(release.Version) {
			continue
		}

		minor := minorVersion(release.Version)
		if supported[minor] {
			continue
		}

		branches = append(branches, EOLBranch{
			Minor:       minor,
			LastRelease: release.Version,
		})
	}

	sort.Slice(branches, func(i, j int) bool {
		return compareVersion(branches[i].Minor+".0", branches[j].Minor+".0") > 0
	})

	return branches, nil
}

type phpNetRelease struct {
	Version           string   `json:"version"`
	SupportedVersions []string `json:"supported_versions"`
}
