package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"herdlite/internal/paths"
	"herdlite/internal/state"
)

func TestMailAPIListDetailAndClear(t *testing.T) {
	root := t.TempDir()
	p := paths.Paths{StateFile: filepath.Join(root, "config", "herdlite.db")}
	service := Service{Paths: p, Store: state.NewStore(p.StateFile), Token: "test-token"}
	mux := http.NewServeMux()
	service.registerMailAPIHandlers(mux)

	id, err := service.Store.AddMailMessage(state.MailMessage{
		ProjectName: "demo",
		Sender:      "from@example.test",
		Recipients:  "to@example.test",
		Subject:     "Hello",
		TextBody:    "Plain",
		HTMLBody:    "<p>Hello</p>",
		RawMIME:     []byte("Subject: Hello\r\n\r\nPlain"),
		Attachments: []state.MailAttachment{{
			Filename:    "notes.txt",
			ContentType: "text/plain",
			Content:     []byte("notes"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	list := httptest.NewRecorder()
	mux.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/mail", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", list.Code)
	}
	var summaries []mailSummaryResponse
	if err := json.Unmarshal(list.Body.Bytes(), &summaries); err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].ID != id || summaries[0].AttachmentCount != 1 {
		t.Fatalf("unexpected summaries: %+v", summaries)
	}

	detail := httptest.NewRecorder()
	mux.ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/api/mail/"+strconvID(id), nil))
	if detail.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", detail.Code)
	}
	var message mailDetailResponse
	if err := json.Unmarshal(detail.Body.Bytes(), &message); err != nil {
		t.Fatal(err)
	}
	if message.HTMLBody != "<p>Hello</p>" || message.Attachments[0].URL != "/mail/1/attachments/1" {
		t.Fatalf("unexpected detail response: %+v", message)
	}

	deleteForbidden := httptest.NewRecorder()
	mux.ServeHTTP(deleteForbidden, httptest.NewRequest(http.MethodDelete, "/api/mail/"+strconvID(id), nil))
	if deleteForbidden.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden delete without token, got %d", deleteForbidden.Code)
	}

	deleteOne := httptest.NewRecorder()
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/mail/"+strconvID(id), nil)
	deleteRequest.Header.Set("X-Herdlite-Token", "test-token")
	mux.ServeHTTP(deleteOne, deleteRequest)
	if deleteOne.Code != http.StatusOK {
		t.Fatalf("expected delete ok, got %d", deleteOne.Code)
	}

	forbidden := httptest.NewRecorder()
	mux.ServeHTTP(forbidden, httptest.NewRequest(http.MethodPost, "/api/mail/clear", nil))
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden without token, got %d", forbidden.Code)
	}

	clear := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/mail/clear", nil)
	request.Header.Set("X-Herdlite-Token", "test-token")
	mux.ServeHTTP(clear, request)
	if clear.Code != http.StatusOK {
		t.Fatalf("expected clear ok, got %d", clear.Code)
	}
	messages, err := service.Store.MailMessages(state.MailFilter{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 0 {
		t.Fatalf("expected messages to be cleared, got %d", len(messages))
	}
}

func strconvID(id int64) string {
	return strconv.FormatInt(id, 10)
}
