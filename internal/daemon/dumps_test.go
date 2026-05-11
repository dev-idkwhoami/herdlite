package daemon

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"herdlite/internal/paths"
	"herdlite/internal/state"
)

func TestDumpAPIStoresRenderedHTML(t *testing.T) {
	root := t.TempDir()
	p := paths.Paths{
		StateFile: filepath.Join(root, "config", "herdlite.db"),
	}
	service := Service{Paths: p, Store: state.NewStore(p.StateFile), Token: "test-token"}
	mux := http.NewServeMux()
	service.registerDumpHandlers(mux)

	body := `{"project_name":"demo","project_path":"/tmp/demo","sapi":"cli","command":"artisan test","file":"/tmp/demo/routes/web.php:5","html":"<div class=\"sf-dump\">ok</div>"}`
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/dumps", strings.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	list := httptest.NewRecorder()
	mux.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/dumps", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", list.Code)
	}
	if !strings.Contains(list.Body.String(), "sf-dump") {
		t.Fatalf("expected rendered dump HTML, got %s", list.Body.String())
	}
}
