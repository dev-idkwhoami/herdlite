package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"herdlite/internal/paths"
	"herdlite/internal/state"
)

func TestStatusRemovesStalePID(t *testing.T) {
	root := t.TempDir()
	p := paths.Paths{
		RuntimeDir: filepath.Join(root, "runtime"),
		LogDir:     filepath.Join(root, "logs"),
		StateFile:  filepath.Join(root, "config", "herdlite.db"),
	}
	manager := Manager{Paths: p, Store: state.NewStore(p.StateFile)}
	if err := os.MkdirAll(p.RuntimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manager.PIDPath(), []byte("999999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	status := manager.Status()
	if status.Running {
		t.Fatal("expected stale daemon to be stopped")
	}
	if _, err := os.Stat(manager.PIDPath()); !os.IsNotExist(err) {
		t.Fatalf("expected stale pid to be removed, got %v", err)
	}
}
