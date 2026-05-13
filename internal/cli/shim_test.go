package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathWithoutDirRemovesShimDir(t *testing.T) {
	sep := string(os.PathListSeparator)
	shims := filepath.Join("home", "user", ".local", "share", "herdlite", "shims")
	pathValue := strings.Join([]string{
		filepath.Join("usr", "local", "bin"),
		shims,
		filepath.Join("home", "user", ".nvm", "versions", "node", "v26.1.0", "bin"),
		shims,
		filepath.Join("usr", "bin"),
	}, sep)

	got := pathWithoutDir(pathValue, shims)
	want := strings.Join([]string{
		filepath.Join("usr", "local", "bin"),
		filepath.Join("home", "user", ".nvm", "versions", "node", "v26.1.0", "bin"),
		filepath.Join("usr", "bin"),
	}, sep)

	if got != want {
		t.Fatalf("pathWithoutDir() = %q, want %q", got, want)
	}
}
