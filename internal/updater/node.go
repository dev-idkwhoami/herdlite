package updater

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var nodeVersion = regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)$`)

// NodeSources selects the newest installed patch in each major, retaining old
// installations and exact .nvmrc/default pins.
func NodeSources(dir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(dir, "versions", "node"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	latest := map[int]string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		match := nodeVersion.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		major, _ := strconv.Atoi(match[1])
		if latest[major] == "" || newerNode(entry.Name(), latest[major]) {
			latest[major] = entry.Name()
		}
	}
	majors := make([]int, 0, len(latest))
	for major := range latest {
		majors = append(majors, major)
	}
	sort.Ints(majors)
	var versions []string
	for _, major := range majors {
		versions = append(versions, latest[major])
	}
	return versions, nil
}
func newerNode(a, b string) bool {
	aa, bb := nodeVersion.FindStringSubmatch(a), nodeVersion.FindStringSubmatch(b)
	for n := 1; n <= 3; n++ {
		x, _ := strconv.Atoi(aa[n])
		y, _ := strconv.Atoi(bb[n])
		if x != y {
			return x > y
		}
	}
	return false
}
func NVMDir() (string, error) {
	if dir := os.Getenv("NVM_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	return filepath.Join(home, ".nvm"), err
}
func UpdateNode(ctx context.Context, dir string, versions []string, out, errOut io.Writer) error {
	// No login shell: unrelated shell startup commands must not run during updates.
	script := ` . "$NVM_DIR/nvm.sh" --no-use || exit
old="$1"
major="${old%%.*}"
target="$(nvm version-remote "$major")" || exit
case "$target" in v[0-9]*.[0-9]*.[0-9]*) ;; *) echo "Cannot resolve Node $major" >&2; exit 1;; esac
if [ "$target" = "$old" ]; then echo "Node $old is current"; exit 0; fi
nvm install "$target" --reinstall-packages-from="$old"
`
	for _, version := range versions {
		fmt.Fprintf(out, "Updating Node %s within major %s\n", version, strings.Split(version, ".")[0])
		cmd := exec.CommandContext(ctx, "bash", "-c", script, "herdlite-node-update", version)
		cmd.Env = append(os.Environ(), "NVM_DIR="+dir)
		cmd.Stdout = out
		cmd.Stderr = errOut
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	return nil
}
