package phpmanager

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestResolveRelease(t *testing.T) {
	releases := []Release{
		{Version: "8.5.6", Minor: "8.5", Tag: "php-8.5.6", Published: time.Now()},
		{Version: "8.4.21", Minor: "8.4", Tag: "php-8.4.21", Published: time.Now()},
		{Version: "8.4.20", Minor: "8.4", Tag: "php-8.4.20", Published: time.Now()},
	}

	tests := []struct {
		name      string
		requested string
		want      string
	}{
		{name: "empty resolves latest", requested: "", want: "8.5.6"},
		{name: "latest resolves latest", requested: "latest", want: "8.5.6"},
		{name: "minor resolves newest patch", requested: "8.4", want: "8.4.21"},
		{name: "exact resolves exact", requested: "8.4.20", want: "8.4.20"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveRelease(releases, tt.requested)
			if err != nil {
				t.Fatal(err)
			}
			if got.Version != tt.want {
				t.Fatalf("expected %s, got %s", tt.want, got.Version)
			}
		})
	}
}

func TestLatestByMinor(t *testing.T) {
	releases := []Release{
		{Version: "8.5.6", Minor: "8.5", Tag: "php-8.5.6"},
		{Version: "8.4.21", Minor: "8.4", Tag: "php-8.4.21"},
		{Version: "8.4.20", Minor: "8.4", Tag: "php-8.4.20"},
		{Version: "8.3.31", Minor: "8.3", Tag: "php-8.3.31"},
	}

	got := LatestByMinor(releases)
	if len(got) != 3 {
		t.Fatalf("expected 3 minor releases, got %d", len(got))
	}
	if got[1].Version != "8.4.21" {
		t.Fatalf("expected newest 8.4 patch, got %s", got[1].Version)
	}
}

func TestReleasePaginationIncludesOlderMinors(t *testing.T) {
	calls := 0
	client := ReleaseClient{HTTPClient: &http.Client{Transport: releaseTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		header := http.Header{}
		body := `[{"tag_name":"php-8.5.12"},{"tag_name":"php-8.6.0RC1","prerelease":true}]`
		if req.URL.Query().Get("page") == "1" {
			header.Set("Link", `<https://api.github.com/next>; rel="next"`)
		} else {
			body = `[{"tag_name":"php-8.3.30"}]`
		}
		return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	releases, err := client.Releases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(releases) != 2 {
		t.Fatalf("pagination: %d calls, %+v", calls, releases)
	}
	release, err := ResolveRelease(releases, "8.3")
	if err != nil || release.Version != "8.3.30" {
		t.Fatal(release, err)
	}
}

type releaseTransport func(*http.Request) (*http.Response, error)

func (f releaseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
