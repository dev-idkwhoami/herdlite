package daemon

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdlite/internal/paths"
	"herdlite/internal/state"
)

func TestQueuesAPIListsLinkedProjects(t *testing.T) {
	root := t.TempDir()
	projectPath := filepath.Join(root, "app")
	if err := os.MkdirAll(filepath.Join(projectPath, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectPath, "public", "index.php"), []byte("<?php\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := paths.Paths{
		LogDir:     filepath.Join(root, "logs"),
		RuntimeDir: filepath.Join(root, "runtime"),
		StateFile:  filepath.Join(root, "state.db"),
	}
	store := state.NewStore(p.StateFile)
	if _, err := store.AddProjectWithOptions(projectPath, state.ProjectOptions{Name: "demo", Domain: "demo.test"}); err != nil {
		t.Fatal(err)
	}

	service := Service{Paths: p, Store: store, Token: "token"}
	handler := http.NewServeMux()
	service.registerQueueHandlers(handler)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/queues", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"project_name":"demo"`) || !strings.Contains(body, `"running":false`) {
		t.Fatalf("expected stopped demo queue worker, got %s", body)
	}
}

func TestQueueActionRequiresToken(t *testing.T) {
	root := t.TempDir()
	p := paths.Paths{StateFile: filepath.Join(root, "state.db")}
	store := state.NewStore(p.StateFile)
	projectPath := filepath.Join(root, "app")
	if err := os.MkdirAll(filepath.Join(projectPath, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectPath, "public", "index.php"), []byte("<?php\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddProjectWithOptions(projectPath, state.ProjectOptions{Name: "demo", Domain: "demo.test"}); err != nil {
		t.Fatal(err)
	}

	service := Service{Paths: p, Store: store, Token: "token"}
	handler := http.NewServeMux()
	service.registerQueueHandlers(handler)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/queues/demo/start", nil))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", recorder.Code)
	}
}
