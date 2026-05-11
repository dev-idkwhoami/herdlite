package mail

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"herdlite/internal/state"
)

func TestHTMLViewerServesStoredHTMLWithCSP(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "herdlite.db"))
	id, err := store.AddMailMessage(state.MailMessage{
		ProjectName: "app",
		Sender:      "noreply@app.test",
		Recipients:  "user@test",
		Subject:     "HTML",
		HTMLBody:    "<strong>Hello</strong>",
		RawMIME:     []byte("Subject: HTML\r\n\r\n<strong>Hello</strong>"),
		Attachments: []state.MailAttachment{{
			Filename:    "notes.txt",
			ContentType: "text/plain",
			Size:        5,
			Content:     []byte("notes"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/mail/1/html", nil)
	recorder := httptest.NewRecorder()
	(Service{Store: store}).httpHandler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "<strong>Hello</strong>") {
		t.Fatalf("expected HTML body, got %q", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "Herdlite Mail") {
		t.Fatalf("expected mail client shell, got %q", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "/mail/1/attachments/1") {
		t.Fatalf("expected attachment link, got %q", recorder.Body.String())
	}
	if recorder.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("expected CSP header")
	}
	if id != 1 {
		t.Fatalf("expected id 1, got %d", id)
	}
}

func TestHTMLViewerServesAttachment(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "herdlite.db"))
	if _, err := store.AddMailMessage(state.MailMessage{
		ProjectName: "app",
		Sender:      "noreply@app.test",
		Recipients:  "user@test",
		Subject:     "Attachment",
		RawMIME:     []byte("Subject: Attachment\r\n\r\n"),
		Attachments: []state.MailAttachment{{
			Filename:    "notes.txt",
			ContentType: "text/plain",
			Content:     []byte("notes"),
		}},
	}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/mail/1/attachments/1", nil)
	recorder := httptest.NewRecorder()
	(Service{Store: store}).httpHandler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	if recorder.Body.String() != "notes" {
		t.Fatalf("expected attachment bytes, got %q", recorder.Body.String())
	}
	if recorder.Header().Get("Content-Type") != "text/plain" {
		t.Fatalf("expected text/plain, got %q", recorder.Header().Get("Content-Type"))
	}
	if !strings.Contains(recorder.Header().Get("Content-Disposition"), "notes.txt") {
		t.Fatalf("expected attachment filename, got %q", recorder.Header().Get("Content-Disposition"))
	}
}
