package phpmanager

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"herdlite/internal/state"
)

const eolURL = "https://www.php.net/eol.php"

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
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	rows := htmlRows(string(data))

	branches := []EOLBranch{}
	for _, row := range rows {
		if len(row) < 3 {
			continue
		}

		minor := firstVersion(row[0], `^\d+\.\d+$`)
		if minor == "" {
			continue
		}

		lastRelease := ""
		for _, cell := range row[2:] {
			lastRelease = firstVersion(cell, `\d+\.\d+\.\d+`)
			if lastRelease != "" {
				break
			}
		}

		branches = append(branches, EOLBranch{
			Minor:       minor,
			EOLDate:     normalizeSpaces(row[1]),
			LastRelease: lastRelease,
		})
	}

	return branches, nil
}

func htmlRows(input string) [][]string {
	rowPattern := regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	cellPattern := regexp.MustCompile(`(?is)<t[dh][^>]*>(.*?)</t[dh]>`)
	tagPattern := regexp.MustCompile(`(?is)<[^>]+>`)

	rows := [][]string{}
	for _, rowMatch := range rowPattern.FindAllStringSubmatch(input, -1) {
		cells := []string{}
		for _, cellMatch := range cellPattern.FindAllStringSubmatch(rowMatch[1], -1) {
			text := tagPattern.ReplaceAllString(cellMatch[1], " ")
			text = strings.ReplaceAll(text, "&nbsp;", " ")
			text = strings.ReplaceAll(text, "&#039;", "'")
			text = strings.ReplaceAll(text, "&amp;", "&")
			cells = append(cells, normalizeSpaces(text))
		}
		if len(cells) > 0 {
			rows = append(rows, cells)
		}
	}
	return rows
}

func normalizeSpaces(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func firstVersion(value string, pattern string) string {
	re := regexp.MustCompile(pattern)
	return re.FindString(value)
}
