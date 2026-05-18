package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"herdlite/internal/queue"
)

func (s Service) registerQueueHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/queues", s.serveQueuesAPI)
	mux.HandleFunc("/api/queues/", s.serveQueueRoute)
}

func (s Service) serveQueuesAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	statuses, err := (queue.Manager{Paths: s.Paths, Store: s.Store, Out: s.Out}).Statuses()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(statuses)
}

func (s Service) serveQueueRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("X-Herdlite-Token") != s.Token || s.Token == "" {
		http.Error(w, "invalid token", http.StatusForbidden)
		return
	}

	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/queues/"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	projectName, err := url.PathUnescape(parts[0])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	action := parts[1]
	project, found, err := s.Store.ProjectByNameOrDomain(projectName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}

	manager := queue.Manager{Paths: s.Paths, Store: s.Store, Out: s.Out}
	var status queue.WorkerStatus
	switch action {
	case "start":
		status, err = manager.Start(context.Background(), project)
	case "stop":
		status, err = manager.Stop(project)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.publish("queue.changed", project.Name)
	s.publish("log.changed", status.LogID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}
