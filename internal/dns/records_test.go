package dns

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdlite/internal/debugui"
	"herdlite/internal/paths"
	"herdlite/internal/state"
)

func TestHostRecordConfigIncludesDebugAndLinkedProjectDomains(t *testing.T) {
	root := t.TempDir()
	projectPath := filepath.Join(root, "app")
	if err := os.MkdirAll(filepath.Join(projectPath, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectPath, "public", "index.php"), []byte("<?php\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := state.NewStore(paths.Paths{StateFile: filepath.Join(root, "state.db")}.StateFile)
	if _, err := store.AddProjectWithOptions(projectPath, state.ProjectOptions{
		Name:             "demo",
		Domain:           "demo.test",
		WebsocketEnabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	content, err := HostRecordConfig(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, domain := range []string{debugui.Domain, "demo.test", "ws.demo.test"} {
		if !strings.Contains(content, "host-record="+domain+",127.0.0.1\n") {
			t.Fatalf("expected config to contain %s, got:\n%s", domain, content)
		}
	}
}
