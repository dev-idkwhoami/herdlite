package phpmanager

import (
	"strings"
	"testing"
)

func TestParseEOLFromPHPReleasesJSON(t *testing.T) {
	input := `{
		"8": {"version": "8.5.1", "supported_versions": ["8.5", "8.4"]},
		"7": {"version": "7.4.33", "supported_versions": ["8.5", "8.4"]},
		"5": {"version": "5.6.40", "supported_versions": ["8.5", "8.4"]},
		"3": {"version": "3.0.x (latest)", "supported_versions": ["8.5", "8.4"]}
	}`

	got, err := ParseEOL(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 unsupported branches, got %+v", got)
	}
	if got[0].Minor != "7.4" || got[0].LastRelease != "7.4.33" || got[0].EOLDate != "" {
		t.Fatalf("unexpected first branch: %+v", got[0])
	}
	if got[1].Minor != "5.6" || got[1].LastRelease != "5.6.40" || got[1].EOLDate != "" {
		t.Fatalf("unexpected second branch: %+v", got[1])
	}
}

func TestParseEOLRejectsMalformedJSON(t *testing.T) {
	if _, err := ParseEOL(strings.NewReader(`not json`)); err == nil {
		t.Fatal("expected malformed JSON error")
	}
}
