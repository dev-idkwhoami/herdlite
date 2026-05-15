package daemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUIServesEmbeddedAppAndFallbackRoutes(t *testing.T) {
	service := Service{}
	mux := http.NewServeMux()
	service.registerUIHandlers(mux)

	root := httptest.NewRecorder()
	mux.ServeHTTP(root, httptest.NewRequest(http.MethodGet, "/", nil))
	if root.Code != http.StatusFound {
		t.Fatalf("expected root redirect, got %d", root.Code)
	}
	if location := root.Header().Get("Location"); location != "/app/" {
		t.Fatalf("expected /app/ redirect, got %q", location)
	}

	app := httptest.NewRecorder()
	mux.ServeHTTP(app, httptest.NewRequest(http.MethodGet, "/app/", nil))
	if app.Code != http.StatusOK {
		t.Fatalf("expected app index, got %d", app.Code)
	}
	if !strings.Contains(app.Body.String(), `<div id="app"></div>`) {
		t.Fatalf("expected Vite app root in index")
	}

	fallback := httptest.NewRecorder()
	mux.ServeHTTP(fallback, httptest.NewRequest(http.MethodGet, "/app/dumps", nil))
	if fallback.Code != http.StatusOK {
		t.Fatalf("expected SPA fallback, got %d", fallback.Code)
	}

	missingAsset := httptest.NewRecorder()
	mux.ServeHTTP(missingAsset, httptest.NewRequest(http.MethodGet, "/app/assets/missing.js", nil))
	if missingAsset.Code != http.StatusNotFound {
		t.Fatalf("expected missing asset 404, got %d", missingAsset.Code)
	}
}
