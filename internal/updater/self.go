package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func fetch(ctx context.Context, url string, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "herdlite-updater")
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err == nil && int64(len(data)) > max {
		return nil, fmt.Errorf("download exceeds size limit")
	}
	return data, err
}

// StageSelf resolves one immutable release tag, verifies its mandatory checksum,
// and validates a staged binary beside the destination before any replacement.
func StageSelf(ctx context.Context, destination, current string) (string, string, error) {
	if _, err := releaseVersion(current); err != nil {
		return "", "", fmt.Errorf("self-update preserves local/development build %q; install a published release explicitly or rebuild this checkout: %w", current, err)
	}
	repo := os.Getenv("HERDLITE_REPO")
	if repo == "" {
		repo = "dev-idkwhoami/herdlite"
	}
	if strings.ContainsAny(repo, "?#\\") || len(strings.Split(repo, "/")) != 2 {
		return "", "", fmt.Errorf("invalid HERDLITE_REPO")
	}
	data, err := fetch(ctx, "https://api.github.com/repos/"+repo+"/releases/latest", 1<<20)
	if err != nil {
		return "", "", err
	}
	var release struct {
		Tag string `json:"tag_name"`
	}
	if err = json.Unmarshal(data, &release); err != nil {
		return "", "", err
	}
	if release.Tag == "" || strings.ContainsAny(release.Tag, "/?#\\") {
		return "", "", fmt.Errorf("invalid release tag")
	}
	if err := checkSelfUpgrade(current, release.Tag); err != nil {
		return "", "", err
	}
	if current == release.Tag {
		return "", release.Tag, nil
	}
	asset := "herdlite-" + runtime.GOOS + "-" + runtime.GOARCH + ".tar.gz"
	base := "https://github.com/" + repo + "/releases/download/" + release.Tag + "/" + asset
	checksum, err := fetch(ctx, base+".sha256", 4096)
	if err != nil {
		return "", "", err
	}
	archive, err := fetch(ctx, base, 256<<20)
	if err != nil {
		return "", "", err
	}
	if err = verifyChecksum(archive, string(checksum)); err != nil {
		return "", "", err
	}
	f, err := os.CreateTemp(filepath.Dir(destination), ".herdlite-update-*")
	if err != nil {
		return "", "", err
	}
	staged := f.Name()
	_ = f.Close()
	success := false
	defer func() {
		if !success {
			_ = os.Remove(staged)
		}
	}()
	if err = extractBinary(archive, staged, strings.TrimSuffix(asset, ".tar.gz")+"/herdlite"); err != nil {
		return "", "", err
	}
	output, err := exec.CommandContext(ctx, staged, "version").CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("validate new executable: %w: %s", err, output)
	}
	if !strings.HasPrefix(string(output), "herdlite "+release.Tag+" (") {
		return "", "", fmt.Errorf("release binary version mismatch: %s", output)
	}
	success = true
	return staged, release.Tag, nil
}
func verifyChecksum(data []byte, checksum string) error {
	fields := strings.Fields(checksum)
	if len(fields) == 0 {
		return fmt.Errorf("empty release checksum")
	}
	expected, err := hex.DecodeString(fields[0])
	if err != nil || len(expected) != sha256.Size {
		return fmt.Errorf("invalid release checksum")
	}
	sum := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), fields[0]) {
		return fmt.Errorf("release checksum mismatch")
	}
	return nil
}
func extractBinary(data []byte, destination, member string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("release archive has no %s", member)
		}
		if err != nil {
			return err
		}
		if h.Name != member {
			continue
		}
		if h.Typeflag != tar.TypeReg || h.Size <= 0 || h.Size > 256<<20 {
			return fmt.Errorf("invalid executable archive entry")
		}
		f, err := os.OpenFile(destination, os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return err
		}
		_, copyErr := io.CopyN(f, tr, h.Size)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		return os.Chmod(destination, 0755)
	}
}

// Only stable release versions have an ordering that can safely drive an update.
// In particular, "dev" is not older than every published release.
func releaseVersion(version string) ([3]uint64, error) {
	var result [3]uint64
	if !strings.HasPrefix(version, "v") {
		return result, fmt.Errorf("expected a stable release version such as v0.5.0")
	}
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	if len(parts) != 3 {
		return result, fmt.Errorf("invalid stable release version %q", version)
	}
	for n, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return result, fmt.Errorf("invalid release version %q", version)
		}
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return result, fmt.Errorf("invalid release version %q", version)
			}
		}
		value, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return result, err
		}
		result[n] = value
	}
	return result, nil
}
func checkSelfUpgrade(current, target string) error {
	from, err := releaseVersion(current)
	if err != nil {
		return err
	}
	to, err := releaseVersion(target)
	if err != nil {
		return err
	}
	for n := range from {
		if to[n] > from[n] {
			return nil
		}
		if to[n] < from[n] {
			return fmt.Errorf("refusing to downgrade Herdlite from %s to %s", current, target)
		}
	}
	return nil
}
