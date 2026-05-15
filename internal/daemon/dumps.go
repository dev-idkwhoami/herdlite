package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"herdlite/internal/state"
)

type dumpRequest struct {
	ProjectName string `json:"project_name"`
	ProjectPath string `json:"project_path"`
	SAPI        string `json:"sapi"`
	URI         string `json:"uri"`
	Command     string `json:"command"`
	File        string `json:"file"`
	HTML        string `json:"html"`
}

type dumpResponse struct {
	ID          int64  `json:"id"`
	ProjectName string `json:"project_name"`
	ProjectPath string `json:"project_path"`
	SAPI        string `json:"sapi"`
	URI         string `json:"uri"`
	Command     string `json:"command"`
	File        string `json:"file"`
	HTML        string `json:"html"`
	CapturedAt  string `json:"captured_at"`
}

func (s Service) registerDumpHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/dumps", s.serveDumpsIndex)
	mux.HandleFunc("/api/dumps", s.serveDumpsAPI)
	mux.HandleFunc("/api/dumps/clear", s.serveDumpsClear)
	mux.HandleFunc("/api/dumps/clear-before/", s.serveDumpsClearBefore)
	mux.HandleFunc("/api/dumps/", s.serveDumpRoute)
}

func (s Service) serveDumpsIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/app/dumps", http.StatusFound)
}

func (s Service) serveDumpsAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.serveDumpsList(w)
	case http.MethodPost:
		s.receiveDump(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s Service) serveDumpsList(w http.ResponseWriter) {
	dumps, err := s.Store.DebugDumps(state.MaxDebugDumps)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]dumpResponse, 0, len(dumps))
	for _, dump := range dumps {
		out = append(out, dumpToResponse(dump))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s Service) receiveDump(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	defer r.Body.Close()

	var request dumpRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(request.HTML) == "" {
		http.Error(w, "missing html", http.StatusBadRequest)
		return
	}

	id, err := s.Store.AddDebugDump(state.DebugDump{
		ProjectName: request.ProjectName,
		ProjectPath: request.ProjectPath,
		SAPI:        request.SAPI,
		URI:         request.URI,
		Command:     request.Command,
		File:        request.File,
		HTML:        request.HTML,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if id == 0 {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
		return
	}
	s.publish("dump.created", strconv.FormatInt(id, 10))
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"id":%d}`, id)
}

func (s Service) serveDumpsClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("X-Herdlite-Token") != s.Token || s.Token == "" {
		http.Error(w, "invalid token", http.StatusForbidden)
		return
	}
	if _, err := s.Store.ClearDebugDumps(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.publish("debug.cleared", "dumps")
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true}`)
}

func (s Service) serveDumpsClearBefore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("X-Herdlite-Token") != s.Token || s.Token == "" {
		http.Error(w, "invalid token", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/dumps/clear-before/"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	count, err := s.Store.ClearDebugDumpsBefore(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if count > 0 {
		s.publish("debug.cleared", "dumps")
	}
	writeJSON(w, map[string]any{"ok": true, "count": count})
}

func (s Service) serveDumpRoute(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/dumps/"), "/"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("X-Herdlite-Token") != s.Token || s.Token == "" {
		http.Error(w, "invalid token", http.StatusForbidden)
		return
	}
	count, err := s.Store.DeleteDebugDump(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if count == 0 {
		http.NotFound(w, r)
		return
	}
	s.publish("debug.cleared", strconv.FormatInt(id, 10))
	writeJSON(w, map[string]any{"ok": true, "count": count})
}

func dumpToResponse(dump state.DebugDump) dumpResponse {
	return dumpResponse{
		ID:          dump.ID,
		ProjectName: dump.ProjectName,
		ProjectPath: dump.ProjectPath,
		SAPI:        dump.SAPI,
		URI:         dump.URI,
		Command:     dump.Command,
		File:        dump.File,
		HTML:        dump.HTML,
		CapturedAt:  dump.CapturedAt.Local().Format("2006-01-02 15:04:05"),
	}
}
