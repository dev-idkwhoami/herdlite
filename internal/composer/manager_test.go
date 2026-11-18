package composer

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdlite/internal/paths"
	"herdlite/internal/state"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestInstallPreservesOldComposerOnFailure(t *testing.T) {
	for _, failure := range []string{"download", "validation", "none"} {
		t.Run(failure, func(t *testing.T) {
			p := paths.ResolveForHome(t.TempDir())
			m := Manager{Paths: p, Out: io.Discard}
			_ = os.MkdirAll(p.ComposerDir, 0755)
			_ = os.WriteFile(m.Path(), []byte("old"), 0755)
			php := filepath.Join(t.TempDir(), "php")
			script := "#!/bin/sh\nexit 0\n"
			if failure == "validation" {
				script = "#!/bin/sh\nexit 1\n"
			}
			_ = os.WriteFile(php, []byte(script), 0755)
			previous := http.DefaultClient
			t.Cleanup(func() { http.DefaultClient = previous })
			http.DefaultClient = &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
				if failure == "download" {
					return nil, errors.New("offline")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("new")), Header: make(http.Header)}, nil
			})}
			err := m.Install(context.Background(), state.PHPRuntime{PHPBinary: php}, false)
			if (err != nil) != (failure != "none") {
				t.Fatalf("unexpected error: %v", err)
			}
			got, _ := os.ReadFile(m.Path())
			want := "old"
			if failure == "none" {
				want = "new"
			}
			if string(got) != want {
				t.Fatalf("got %q want %q", got, want)
			}
			files, _ := os.ReadDir(p.ComposerDir)
			if len(files) != 1 {
				t.Fatalf("staged files left behind: %v", files)
			}
		})
	}
}
