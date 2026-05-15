package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"herdlite/internal/state"
)

type mailSummaryResponse struct {
	ID              int64  `json:"id"`
	ProjectName     string `json:"project_name"`
	Sender          string `json:"sender"`
	ReplyTo         string `json:"reply_to"`
	Recipients      string `json:"recipients"`
	Subject         string `json:"subject"`
	ReceivedAt      string `json:"received_at"`
	AttachmentCount int    `json:"attachment_count"`
	HasHTML         bool   `json:"has_html"`
	HasText         bool   `json:"has_text"`
}

type mailDetailResponse struct {
	mailSummaryResponse
	TextBody    string                   `json:"text_body"`
	HTMLBody    string                   `json:"html_body"`
	RawMIME     string                   `json:"raw_mime"`
	Attachments []mailAttachmentResponse `json:"attachments"`
}

type mailAttachmentResponse struct {
	ID          int64  `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	URL         string `json:"url"`
}

func (s Service) registerMailAPIHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/mail", s.serveMailAPI)
	mux.HandleFunc("/api/mail/", s.serveMailRoute)
}

func (s Service) serveMailAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		messages, err := s.Store.MailMessages(state.MailFilter{All: true})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out := make([]mailSummaryResponse, 0, len(messages))
		for _, message := range messages {
			out = append(out, mailSummary(message))
		}
		writeJSON(w, out)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s Service) serveMailRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/mail/")
	if rest == "clear" {
		s.serveMailClear(w, r)
		return
	}
	id, err := strconv.ParseInt(strings.Trim(rest, "/"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodDelete {
		s.serveMailDelete(w, r, id)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	message, found, err := s.Store.MailMessage(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, mailDetail(message))
}

func (s Service) serveMailDelete(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Header.Get("X-Herdlite-Token") != s.Token || s.Token == "" {
		http.Error(w, "invalid token", http.StatusForbidden)
		return
	}
	count, err := s.Store.DeleteMailMessage(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if count == 0 {
		http.NotFound(w, r)
		return
	}
	s.publish("mail.cleared", strconv.FormatInt(id, 10))
	writeJSON(w, map[string]any{"ok": true, "count": count})
}

func (s Service) serveMailClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("X-Herdlite-Token") != s.Token || s.Token == "" {
		http.Error(w, "invalid token", http.StatusForbidden)
		return
	}
	count, err := s.Store.ClearMailMessages(state.MailFilter{All: true})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.publish("mail.cleared", "")
	writeJSON(w, map[string]any{"ok": true, "count": count})
}

func mailSummary(message state.MailMessage) mailSummaryResponse {
	return mailSummaryResponse{
		ID:              message.ID,
		ProjectName:     message.ProjectName,
		Sender:          message.Sender,
		ReplyTo:         message.ReplyTo,
		Recipients:      message.Recipients,
		Subject:         message.Subject,
		ReceivedAt:      message.ReceivedAt.Local().Format("2006-01-02 15:04:05"),
		AttachmentCount: len(message.Attachments),
		HasHTML:         message.HTMLBody != "",
		HasText:         message.TextBody != "",
	}
}

func mailDetail(message state.MailMessage) mailDetailResponse {
	attachments := make([]mailAttachmentResponse, 0, len(message.Attachments))
	for _, attachment := range message.Attachments {
		attachments = append(attachments, mailAttachmentResponse{
			ID:          attachment.ID,
			Filename:    attachment.Filename,
			ContentType: attachment.ContentType,
			Size:        attachment.Size,
			URL:         fmt.Sprintf("/mail/%d/attachments/%d", message.ID, attachment.ID),
		})
	}
	return mailDetailResponse{
		mailSummaryResponse: mailSummary(message),
		TextBody:            message.TextBody,
		HTMLBody:            message.HTMLBody,
		RawMIME:             string(message.RawMIME),
		Attachments:         attachments,
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
