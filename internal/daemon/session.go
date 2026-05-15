package daemon

import "net/http"

func (s Service) registerSessionHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/session", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, map[string]string{"token": s.Token})
	})
}
