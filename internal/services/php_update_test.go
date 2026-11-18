package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"herdlite/internal/paths"
	"herdlite/internal/state"
)

func TestRestoreOfflineServicesStaysOffline(t *testing.T) {
	p := paths.ResolveForHome(t.TempDir())
	m := HostingManager{Paths: p, Store: state.NewStore(p.StateFile)}
	before, err := m.CapturePHPState()
	if err != nil {
		t.Fatal(err)
	}
	if before.Nginx || before.Daemon || len(before.FPM) != 0 || len(before.Queues) != 0 {
		t.Fatal(before)
	}
	// Empty PATH ensures accidental service startup would fail this test.
	t.Setenv("PATH", t.TempDir())
	if err := m.RestorePHP(context.Background(), before, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.NginxDir); !os.IsNotExist(err) {
		t.Fatal("restoration wrote nginx configuration")
	}
}

func TestRestoreMappedFPMPreservesConfiguration(t *testing.T) {
	p := paths.ResolveForHome(t.TempDir())
	prefix := filepath.Join(p.PHPDir, "8.5.12")
	for _, dir := range []string{"etc", "sbin", "var/run"} {
		if err := os.MkdirAll(filepath.Join(prefix, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	config := filepath.Join(prefix, "etc/php-fpm.conf")
	original := []byte("; custom settings must survive restoration\n")
	if err := os.WriteFile(config, original, 0600); err != nil {
		t.Fatal(err)
	}
	// A child owned by the test provides a live PID for the launcher fixture.
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	marker := filepath.Join(t.TempDir(), "launched")
	pidFile := filepath.Join(prefix, "var/run/php-fpm.pid")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = '-t' ]; then exit 0; fi\nprintf '%%s' '%d' > '%s'\nprintf launched >> '%s'\n", child.Process.Pid, pidFile, marker)
	binary := filepath.Join(prefix, "sbin/php-fpm")
	if err := os.WriteFile(binary, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	target := state.PHPRuntime{Version: "8.5.12", Prefix: prefix, PHPFPMBinary: binary}
	m := HostingManager{Paths: p, Store: state.NewStore(p.StateFile)}
	before := MaintenanceState{FPM: []state.PHPRuntime{{Version: "8.5.10"}, {Version: "8.5.11"}}}
	if err := m.RestorePHP(context.Background(), before, map[string]state.PHPRuntime{"8.5.10": target, "8.5.11": target}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(config)
	if err != nil || string(data) != string(original) {
		t.Fatal("configuration overwritten")
	}
	data, err = os.ReadFile(marker)
	if err != nil || string(data) != "launched" {
		t.Fatalf("FPM started more than once: %s %v", data, err)
	}
	data, err = os.ReadFile(pidFile)
	if err != nil || strings.TrimSpace(string(data)) != strconv.Itoa(child.Process.Pid) {
		t.Fatal("missing PID")
	}
}
