package daemon

import (
	"bytes"
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

func TestQueueSettingsCanBeUpdated(t *testing.T) {
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
	if _, err := store.AddProjectWithOptions(projectPath, state.ProjectOptions{Name: "demo", Domain: "demo.test", PHPVersion: "8.4"}); err != nil {
		t.Fatal(err)
	}

	service := Service{Paths: p, Store: store, Token: "token"}
	handler := http.NewServeMux()
	service.registerQueueHandlers(handler)

	body := bytes.NewBufferString(`{"connection":"redis","queue":"critical,default","processes":2,"backoff":1,"sleep":5,"rest":0,"timeout":90,"tries":2,"memory":256,"env":"local","force":true}`)
	request := httptest.NewRequest(http.MethodPost, "/api/queues/demo/settings", body)
	request.Header.Set("X-Herdlite-Token", "token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	response := recorder.Body.String()
	if !strings.Contains(response, `"connection":"redis"`) || !strings.Contains(response, `"queue":"critical,default"`) || !strings.Contains(response, `"processes":2`) {
		t.Fatalf("expected updated settings in response, got %s", response)
	}
	if !strings.Contains(response, `php artisan queue:work redis --queue=critical,default`) {
		t.Fatalf("expected command preview in response, got %s", response)
	}
}
