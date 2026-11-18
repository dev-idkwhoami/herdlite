package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdlite/internal/app"
	"herdlite/internal/paths"
	"herdlite/internal/state"
)

func updateTestApp(t *testing.T) (*app.App, *bytes.Buffer) {
	t.Helper()
	p := paths.ResolveForHome(t.TempDir())
	out := &bytes.Buffer{}
	return &app.App{Paths: p, Store: state.NewStore(p.StateFile), Out: out, Err: out}, out
}
func TestUpdateAliasesAndValidation(t *testing.T) {
	a, out := updateTestApp(t)
	for _, name := range []string{"update", "upgrade", "nginx", "dns", "system", "self"} {
		found := false
		for _, cmd := range allCommands() {
			if cmd.Name == name {
				found = true
				if cmd.Run(a, []string{"--help"}) != 0 {
					t.Fatal(name)
				}
			}
		}
		if !found {
			t.Fatal(name)
		}
	}
	for _, args := range [][]string{{"unknown"}, {"all", "node"}, {"nginx", "--force"}, {"--yes"}} {
		if runUpdate(a, args) == 0 {
			t.Fatalf("accepted %v", args)
		}
	}
	if _, err := os.Stat(a.Paths.ConfigDir); !os.IsNotExist(err) {
		t.Fatalf("help/invalid args created state: %v; %s", err, out)
	}
}
func TestComponentDryRunsDoNotCreateState(t *testing.T) {
	a, _ := updateTestApp(t)
	t.Setenv("NVM_DIR", filepath.Join(t.TempDir(), "missing"))
	for _, component := range []string{"composer", "node", "self", "php"} {
		if runUpdate(a, []string{component, "--dry-run"}) != 0 {
			t.Fatal(component)
		}
	}
	if _, err := os.Stat(a.Paths.ConfigDir); !os.IsNotExist(err) {
		t.Fatalf("dry run created config: %v", err)
	}
}
func TestPackageDryRunScopedAndMajorGuard(t *testing.T) {
	a, out := updateTestApp(t)
	bin := t.TempDir()
	log := filepath.Join(bin, "calls")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$TEST_CALLS"
case "$1" in
-Qq) test "$2" = nginx ;;
-Sp) printf 'nginx 1.28.0-1\npostgresql 18.0-1\n' ;;
*) exit 99 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "pacman"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("TEST_CALLS", log)
	if err := updatePackages(context.Background(), a, "nginx", true); err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(log)
	for _, forbidden := range []string{"-Sy", "-Su", "sudo", "yay"} {
		if strings.Contains(string(calls), forbidden) {
			t.Fatalf("unexpected mutation: %s", calls)
		}
	}
	if !strings.Contains(string(calls), "-Sp --needed --print-format %n %v nginx") {
		t.Fatalf("incorrect targets: %s", calls)
	}
	cluster := filepath.Join(a.Paths.PostgresDir, "data")
	_ = os.MkdirAll(cluster, 0755)
	_ = os.WriteFile(filepath.Join(cluster, "PG_VERSION"), []byte("17\n"), 0600)
	if err := updatePackages(context.Background(), a, "nginx", true); err == nil {
		t.Fatalf("accepted PostgreSQL major dependency change: %s", out)
	}
}
