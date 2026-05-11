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
