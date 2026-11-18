package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdlite/internal/app"
	"herdlite/internal/paths"
	"herdlite/internal/state"
)

func TestPHPExtensionsSyncDryRun(t *testing.T) {
	p := paths.ResolveForHome(t.TempDir())
	store := state.NewStore(p.StateFile)
	if err := store.UpsertPHPRuntime(state.PHPRuntime{Version: "8.5.6", Minor: "8.5", Prefix: "/unused"}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	a := &app.App{Paths: p, Store: store, Out: &output, Err: &output}
	if code := runPHP(a, []string{"extensions", "sync", "8.5", "--dry-run"}); code != 0 {
		t.Fatalf("exit %d: %s", code, output.String())
	}
	if !strings.Contains(output.String(), "PHP 8.5.6: no extensions configured") {
		t.Fatal(output.String())
	}
	if _, err := os.Stat(filepath.Join(p.ConfigDir, "extensions.toml")); !os.IsNotExist(err) {
		t.Fatal("dry run created manifest")
	}
	for _, args := range [][]string{{"sync", "9.9"}, {"sync", "8.5", "--unknown"}, {"sync"}} {
		if code := runPHPExtensions(a, args); code != 1 {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestPHPExtensionsOpenPreservesInvalidConfig(t *testing.T) {
	p := paths.ResolveForHome(t.TempDir())
	var output bytes.Buffer
	a := &app.App{Paths: p, Out: &output, Err: &output}
	bin := t.TempDir()
	capture := filepath.Join(bin, "opened")
	t.Setenv("OPENED_PATH", capture)
	if err := os.WriteFile(filepath.Join(bin, "xdg-open"), []byte("#!/bin/sh\nprintf '%s' \"$1\" > \"$OPENED_PATH\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if code := runPHPExtensions(a, []string{"open"}); code != 0 {
		t.Fatal(output.String())
	}
	path := filepath.Join(p.ConfigDir, "extensions.toml")
	got, err := os.ReadFile(capture)
	if err != nil || string(got) != path {
		t.Fatalf("opened %q: %v", got, err)
	}
	if err := os.WriteFile(path, []byte("[broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := runPHPExtensions(a, []string{"open"}); code != 0 {
		t.Fatal(output.String())
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "[broken" {
		t.Fatal("overwrote existing config")
	}
	if err := os.WriteFile(filepath.Join(bin, "xdg-open"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if code := runPHPExtensions(a, []string{"open"}); code != 1 {
		t.Fatal("ignored opener error")
	}
}

func TestPHPExtensionsListReportsActualModulesAndContinuesOnFailure(t *testing.T) {
	p := paths.ResolveForHome(t.TempDir())
	store := state.NewStore(p.StateFile)
	var output bytes.Buffer
	a := &app.App{Paths: p, Store: store, Out: &output, Err: &output}
	if code := runPHPExtensions(a, []string{"list"}); code != 0 || !strings.Contains(output.String(), "No PHP runtimes") {
		t.Fatal(output.String())
	}
	for _, version := range []string{"8.5.6", "8.5.11", "8.4.1"} {
		bin := filepath.Join(t.TempDir(), "php")
		script := "#!/bin/sh\nprintf '%s' '[\"zlib\",\"Core\"]'\n"
		if version == "8.5.11" {
			script = "#!/bin/sh\nprintf '%s' '[\"zlib\",\"grpc\",\"Core\"]'\n"
		}
		if version == "8.4.1" {
			script = "#!/bin/sh\nexit 1\n"
		}
		if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		if err := store.UpsertPHPRuntime(state.PHPRuntime{Version: version, PHPBinary: bin, Prefix: filepath.Dir(bin)}); err != nil {
			t.Fatal(err)
		}
	}
	output.Reset()
	if code := runPHPExtensions(a, []string{"list"}); code != 1 {
		t.Fatal("expected partial failure")
	}
	for _, want := range []string{"PHP 8.5.11", "Core, grpc, zlib", "PHP 8.5.6", "Core, zlib", "PHP 8.4.1:"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %q: %s", want, output.String())
		}
	}
	if _, err := os.Stat(filepath.Join(p.ConfigDir, "extensions.toml")); !os.IsNotExist(err) {
		t.Fatal("list created config")
	}
}
