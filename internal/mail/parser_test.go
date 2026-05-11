package mail

import (
	"strings"
	"testing"

	"herdlite/internal/state"
)

func TestParseMultipartMessageWithAttachment(t *testing.T) {
	raw := strings.ReplaceAll(`From: App <noreply@app.test>
To: User <user@test>
Subject: =?UTF-8?Q?Hello_=E2=9C=93?=
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary=mixed

--mixed
Content-Type: multipart/alternative; boundary=alt

--alt
Content-Type: text/plain; charset=utf-8

Plain body
--alt
Content-Type: text/html; charset=utf-8

<strong>HTML body</strong>
--alt--
--mixed
Content-Type: text/plain; name=notes.txt
Content-Disposition: attachment; filename=notes.txt

attachment text
--mixed--
`, "\n", "\r\n")

	parsed, err := ParseMessage([]byte(raw), "bounce@app.test", []string{"user@test"})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Subject != "Hello ✓" {
		t.Fatalf("expected decoded subject, got %q", parsed.Subject)
	}
	if parsed.TextBody != "Plain body" {
		t.Fatalf("expected plain body, got %q", parsed.TextBody)
	}
	if parsed.HTMLBody != "<strong>HTML body</strong>" {
		t.Fatalf("expected HTML body, got %q", parsed.HTMLBody)
	}
	if len(parsed.Attachments) != 1 {
		t.Fatalf("expected one attachment, got %d", len(parsed.Attachments))
	}
	if parsed.Attachments[0].Filename != "notes.txt" {
		t.Fatalf("expected attachment filename, got %q", parsed.Attachments[0].Filename)
	}
}

func TestDetectProjectNameFallsBackToUnknown(t *testing.T) {
	projects := []state.Project{{Name: "app", Domain: "app.test"}}

	matched := DetectProjectName(ParsedMessage{Sender: "App <noreply@app.test>"}, projects)
	if matched != "app" {
		t.Fatalf("expected app, got %s", matched)
	}

	unknown := DetectProjectName(ParsedMessage{Sender: "someone@example.com"}, projects)
	if unknown != state.UnknownProjectName {
		t.Fatalf("expected unknown project, got %s", unknown)
	}
}
