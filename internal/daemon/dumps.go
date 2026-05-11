package daemon

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
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
}

func (s Service) serveDumpsIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self' 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	fmt.Fprint(w, dumpsPage(s.Token))
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
	dumps, err := s.Store.DebugDumps(100)
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
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true}`)
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

func dumpsPage(token string) string {
	return fmt.Sprintf(`<!doctype html>
<html><head><meta charset="utf-8"><title>Herdlite Dumps</title>
<style>
body{margin:0;background:#0f1419;color:#e5e7eb;font-family:Inter,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}.app{display:grid;grid-template-columns:360px 1fr;min-height:100vh}
.side{background:#141b24;border-right:1px solid #263241;padding:18px;overflow:auto}.brand{font-size:20px;font-weight:700;margin-bottom:14px}.actions{display:flex;gap:8px;margin-bottom:14px}
button{background:#192231;color:#e5e7eb;border:1px solid #334155;border-radius:6px;padding:8px 10px;cursor:pointer}.danger{border-color:#7f1d1d;color:#fecaca}.dump{width:100%%;text-align:left;margin:0 0 8px;padding:10px;border:1px solid #263241;border-radius:8px;background:#101820;color:#dbe4ee;cursor:pointer}.dump.active{border-color:#38bdf8;background:#152232}.title{font-weight:700}.meta{color:#9ca3af;font-size:12px;margin-top:4px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.main{min-width:0;display:flex;flex-direction:column}.bar{background:#141b24;border-bottom:1px solid #263241;padding:14px 18px}.bar h1{font-size:18px;margin:0 0 8px}.bar .sub{color:#9ca3af;font-size:13px}.paper{padding:22px;overflow:auto}.dump-html{background:#f8fafc;color:#111827;border-radius:8px;padding:18px;min-height:180px}.empty{color:#9ca3af;padding:28px}
</style></head>
<body><div class="app"><aside class="side"><div class="brand">Herdlite Dumps</div><div class="actions"><button id="refresh">Refresh</button><button id="clear" class="danger">Clear</button></div><div id="list"></div></aside>
<main class="main"><div class="bar"><h1 id="heading">No dump selected</h1><div class="sub" id="details"></div></div><section class="paper"><div id="content" class="empty">Waiting for captured dumps.</div></section></main></div>
<script>
const token=%q; let dumps=[], current=null;
const list=document.getElementById('list'), heading=document.getElementById('heading'), details=document.getElementById('details'), content=document.getElementById('content');
async function load(){dumps=await (await fetch('/api/dumps')).json(); renderList(); if(dumps.length){show(current&&dumps.find(d=>d.id===current)?current:dumps[0].id)}else{heading.textContent='No dump selected';details.textContent='';content.className='empty';content.textContent='Waiting for captured dumps.'}}
function renderList(){list.textContent=''; for(const d of dumps){const b=document.createElement('button'); b.className='dump'+(d.id===current?' active':''); b.onclick=()=>show(d.id); b.innerHTML='<div class="title">'+esc(d.project_name||'Unknown Project')+'</div><div class="meta">'+esc(d.captured_at+' · '+(d.file||d.uri||d.command||d.sapi))+'</div>'; list.appendChild(b)}}
function show(id){current=id; const d=dumps.find(x=>x.id===id); if(!d)return; renderList(); heading.textContent=d.project_name||'Unknown Project'; details.textContent=[d.captured_at,d.sapi,d.file||d.uri||d.command].filter(Boolean).join(' · '); content.className='dump-html'; content.innerHTML=d.html;}
function esc(s){return String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
document.getElementById('refresh').onclick=load;
document.getElementById('clear').onclick=async()=>{if(!confirm('Clear captured dumps?'))return; await fetch('/api/dumps/clear',{method:'POST',headers:{'X-Herdlite-Token':token}}); current=null; await load();};
load(); setInterval(load, 3000);
</script></body></html>`, html.EscapeString(token))
}
