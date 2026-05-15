package daemon

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
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

func TestDumpsPageRedirectsToDebugApp(t *testing.T) {
	service := Service{}
	mux := http.NewServeMux()
	service.registerDumpHandlers(mux)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/dumps", nil))

	if recorder.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", recorder.Code)
	}
	if location := recorder.Header().Get("Location"); location != "/app/dumps" {
		t.Fatalf("expected /app/dumps redirect, got %q", location)
	}
}

func TestDumpClearRequiresToken(t *testing.T) {
	root := t.TempDir()
	p := paths.Paths{StateFile: filepath.Join(root, "config", "herdlite.db")}
	service := Service{Paths: p, Store: state.NewStore(p.StateFile), Token: "test-token"}
	mux := http.NewServeMux()
	service.registerDumpHandlers(mux)

	if _, err := service.Store.AddDebugDump(state.DebugDump{HTML: "<div>debug</div>"}); err != nil {
		t.Fatal(err)
	}

	forbidden := httptest.NewRecorder()
	mux.ServeHTTP(forbidden, httptest.NewRequest(http.MethodPost, "/api/dumps/clear", nil))
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden without token, got %d", forbidden.Code)
	}

	clear := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/dumps/clear", nil)
	request.Header.Set("X-Herdlite-Token", "test-token")
	mux.ServeHTTP(clear, request)
	if clear.Code != http.StatusOK {
		t.Fatalf("expected clear ok, got %d", clear.Code)
	}
	dumps, err := service.Store.DebugDumps(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(dumps) != 0 {
		t.Fatalf("expected dumps to be cleared, got %d", len(dumps))
	}
}

func TestDumpClearBeforeRequiresTokenAndPreservesNewerDumps(t *testing.T) {
	root := t.TempDir()
	p := paths.Paths{StateFile: filepath.Join(root, "config", "herdlite.db")}
	service := Service{Paths: p, Store: state.NewStore(p.StateFile), Token: "test-token"}
	mux := http.NewServeMux()
	service.registerDumpHandlers(mux)

	first, err := service.Store.AddDebugDump(state.DebugDump{HTML: "<div>first</div>"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Store.AddDebugDump(state.DebugDump{HTML: "<div>second</div>"})
	if err != nil {
		t.Fatal(err)
	}
	if first == 0 || second == 0 {
		t.Fatalf("expected stored dumps, got %d and %d", first, second)
	}

	forbidden := httptest.NewRecorder()
	mux.ServeHTTP(forbidden, httptest.NewRequest(http.MethodPost, "/api/dumps/clear-before/2", nil))
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden without token, got %d", forbidden.Code)
	}

	clear := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/dumps/clear-before/2", nil)
	request.Header.Set("X-Herdlite-Token", "test-token")
	mux.ServeHTTP(clear, request)
	if clear.Code != http.StatusOK {
		t.Fatalf("expected clear ok, got %d", clear.Code)
	}
	dumps, err := service.Store.DebugDumps(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(dumps) != 1 || dumps[0].ID != second {
		t.Fatalf("expected only newer dump to remain, got %+v", dumps)
	}
}

func TestDumpDeleteRequiresToken(t *testing.T) {
	root := t.TempDir()
	p := paths.Paths{StateFile: filepath.Join(root, "config", "herdlite.db")}
	service := Service{Paths: p, Store: state.NewStore(p.StateFile), Token: "test-token"}
	mux := http.NewServeMux()
	service.registerDumpHandlers(mux)

	id, err := service.Store.AddDebugDump(state.DebugDump{HTML: "<div>single</div>"})
	if err != nil {
		t.Fatal(err)
	}

	forbidden := httptest.NewRecorder()
	mux.ServeHTTP(forbidden, httptest.NewRequest(http.MethodDelete, "/api/dumps/"+strconv.FormatInt(id, 10), nil))
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden without token, got %d", forbidden.Code)
	}

	deleted := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/dumps/"+strconv.FormatInt(id, 10), nil)
	request.Header.Set("X-Herdlite-Token", "test-token")
	mux.ServeHTTP(deleted, request)
	if deleted.Code != http.StatusOK {
		t.Fatalf("expected delete ok, got %d", deleted.Code)
	}
	dumps, err := service.Store.DebugDumps(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(dumps) != 0 {
		t.Fatalf("expected dump to be deleted, got %+v", dumps)
	}
}
