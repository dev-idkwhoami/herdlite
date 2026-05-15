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

func TestLogViewerRawAndClear(t *testing.T) {
	root := t.TempDir()
	p := paths.Paths{LogDir: filepath.Join(root, "logs"), StateFile: filepath.Join(root, "state.db")}
	if err := os.MkdirAll(p.LogDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(p.LogDir, "daemon.log")
	if err := os.WriteFile(logPath, []byte("error one\ninfo two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	service := Service{Paths: p, Store: state.NewStore(p.StateFile), Token: "token"}
	handler := http.NewServeMux()
	service.registerLogHandlers(handler)

	raw := httptest.NewRecorder()
	handler.ServeHTTP(raw, httptest.NewRequest(http.MethodGet, "/logs/daemon/raw", nil))
	if raw.Code != http.StatusOK || !strings.Contains(raw.Body.String(), "error one") {
		t.Fatalf("expected raw log, code=%d body=%q", raw.Code, raw.Body.String())
	}

	clear := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/logs/daemon/clear", nil)
	request.Header.Set("X-Herdlite-Token", "token")
	handler.ServeHTTP(clear, request)
	if clear.Code != http.StatusOK {
		t.Fatalf("expected clear ok, got %d", clear.Code)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatalf("expected truncated log, got %q", string(data))
	}
}

func TestLogsIndexRedirectsAndAPIListsSources(t *testing.T) {
	root := t.TempDir()
	p := paths.Paths{LogDir: filepath.Join(root, "logs")}
	if err := os.MkdirAll(p.LogDir, 0o755); err != nil {
		t.Fatal(err)
	}
	service := Service{Paths: p, Token: "token"}
	handler := http.NewServeMux()
	service.registerLogHandlers(handler)

	redirect := httptest.NewRecorder()
	handler.ServeHTTP(redirect, httptest.NewRequest(http.MethodGet, "/logs", nil))
	if redirect.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", redirect.Code)
	}
	if location := redirect.Header().Get("Location"); location != "/app/logs" {
		t.Fatalf("expected /app/logs redirect, got %q", location)
	}

	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/logs", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", list.Code)
	}
	if !strings.Contains(list.Body.String(), `"id":"daemon"`) {
		t.Fatalf("expected daemon log in API response, got %s", list.Body.String())
	}
}

func TestLogsIncludesLinkedProjectLaravelLog(t *testing.T) {
	root := t.TempDir()
	projectPath := filepath.Join(root, "felyne")
	publicPath := filepath.Join(projectPath, "public")
	logDir := filepath.Join(projectPath, "storage", "logs")
	if err := os.MkdirAll(publicPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publicPath, "index.php"), []byte("<?php\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "laravel.log"), []byte("[2026-05-15] local.ERROR: boom\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := paths.Paths{LogDir: filepath.Join(root, "logs"), StateFile: filepath.Join(root, "state.db")}
	store := state.NewStore(p.StateFile)
	if _, err := store.AddProjectWithOptions(projectPath, state.ProjectOptions{Name: "felyne-de", Domain: "felyne-de.test"}); err != nil {
		t.Fatal(err)
	}
	service := Service{Paths: p, Store: store, Token: "token"}
	handler := http.NewServeMux()
	service.registerLogHandlers(handler)

	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/logs", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", list.Code)
	}
	if !strings.Contains(list.Body.String(), `"id":"laravel-felyne-de"`) {
		t.Fatalf("expected linked Laravel log in API response, got %s", list.Body.String())
	}

	raw := httptest.NewRecorder()
	handler.ServeHTTP(raw, httptest.NewRequest(http.MethodGet, "/logs/laravel-felyne-de/raw", nil))
	if raw.Code != http.StatusOK || !strings.Contains(raw.Body.String(), "local.ERROR: boom") {
		t.Fatalf("expected raw Laravel log, code=%d body=%q", raw.Code, raw.Body.String())
	}
}

func TestLogViewerRejectsUnknownLog(t *testing.T) {
	p := paths.Paths{LogDir: filepath.Join(t.TempDir(), "logs")}
	service := Service{Paths: p, Token: "token"}
	handler := http.NewServeMux()
	service.registerLogHandlers(handler)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/logs/not-a-known-log/raw", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", recorder.Code)
	}
}
