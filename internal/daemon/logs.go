package daemon

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed assets/vendor/highlightjs/highlight.min.js
var highlightJS string

//go:embed assets/vendor/highlightjs/github-dark.min.css
var highlightCSS string

type logEntry struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Path  string `json:"-"`
}

func (s Service) registerLogHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/assets/highlight.min.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		fmt.Fprint(w, highlightJS)
	})
	mux.HandleFunc("/assets/highlight.css", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		fmt.Fprint(w, highlightCSS)
	})
	mux.HandleFunc("/logs", s.serveLogsIndex)
	mux.HandleFunc("/logs/", s.serveLogRoute)
}

func (s Service) serveLogsIndex(w http.ResponseWriter, _ *http.Request) {
	logs := s.logs()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	fmt.Fprint(w, logPage(logs, s.Token))
}

func (s Service) serveLogRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/logs/")
	if strings.HasSuffix(rest, "/raw") {
		id := strings.TrimSuffix(rest, "/raw")
		s.serveLogRaw(w, id)
		return
	}
	if strings.HasSuffix(rest, "/clear") {
		id := strings.TrimSuffix(rest, "/clear")
		s.serveLogClear(w, r, id)
		return
	}
	http.NotFound(w, r)
}

func (s Service) serveLogRaw(w http.ResponseWriter, id string) {
	entry, ok := s.logByID(id)
	if !ok {
		http.NotFound(w, nil)
		return
	}
	data, err := os.ReadFile(entry.Path)
	if os.IsNotExist(err) {
		data = nil
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(data)
}

func (s Service) serveLogClear(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("X-Herdlite-Token") != s.Token || s.Token == "" {
		http.Error(w, "invalid token", http.StatusForbidden)
		return
	}
	entry, ok := s.logByID(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	file, err := os.OpenFile(entry.Path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = file.Close()
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true}`)
}

func (s Service) logByID(id string) (logEntry, bool) {
	for _, entry := range s.logs() {
		if entry.ID == id {
			return entry, true
		}
	}
	return logEntry{}, false
}

func (s Service) logs() []logEntry {
	out := []logEntry{
		{ID: "daemon", Label: "Daemon", Path: filepath.Join(s.Paths.LogDir, "daemon.log")},
		{ID: "nginx-access", Label: "Nginx Access", Path: filepath.Join(s.Paths.LogDir, "nginx-access.log")},
		{ID: "nginx-error", Label: "Nginx Error", Path: filepath.Join(s.Paths.LogDir, "nginx-error.log")},
		{ID: "postgres", Label: "PostgreSQL", Path: filepath.Join(s.Paths.LogDir, "postgres.log")},
	}
	matches, _ := filepath.Glob(filepath.Join(s.Paths.LogDir, "php-fpm-*.launcher.log"))
	sort.Strings(matches)
	for _, path := range matches {
		name := strings.TrimSuffix(filepath.Base(path), ".launcher.log")
		out = append(out, logEntry{ID: name, Label: strings.ToUpper(strings.ReplaceAll(name, "-", " ")), Path: path})
	}
	return out
}

func logPage(logs []logEntry, token string) string {
	data, _ := json.Marshal(logs)
	first := ""
	if len(logs) > 0 {
		first = logs[0].ID
	}
	return fmt.Sprintf(`<!doctype html>
<html><head><meta charset="utf-8"><title>Herdlite Logs</title>
<link rel="stylesheet" href="/assets/highlight.css">
<style>
body{margin:0;background:#111827;color:#e5e7eb;font-family:Inter,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
.app{display:grid;grid-template-columns:280px 1fr;min-height:100vh}.side{background:#0b1220;padding:18px}.brand{font-size:20px;font-weight:700;margin-bottom:16px}
.log{display:block;width:100%%;text-align:left;border:0;border-radius:6px;background:transparent;color:#cbd5e1;padding:9px 10px;margin-bottom:4px;cursor:pointer}.log.active{background:#1f2937;color:#fff}
.main{min-width:0}.bar{display:flex;gap:8px;align-items:center;padding:14px 18px;background:#172033;border-bottom:1px solid #263244}
input,select,button{background:#0f172a;color:#e5e7eb;border:1px solid #334155;border-radius:6px;padding:8px}button{cursor:pointer}.danger{border-color:#7f1d1d;color:#fecaca}
pre{margin:0;padding:18px;white-space:pre-wrap;word-break:break-word}.meta{color:#94a3b8;margin-left:auto}.empty{padding:28px;color:#94a3b8}
</style></head>
<body><div class="app"><aside class="side"><div class="brand">Herdlite Logs</div><div id="logs"></div></aside>
<main class="main"><div class="bar"><input id="filter" placeholder="Filter"><select id="level"><option value="">All</option><option>error</option><option>warn</option><option>info</option><option>debug</option></select><select id="order"><option value="desc">Newest first</option><option value="asc">Oldest first</option></select><button id="raw">Raw</button><button id="clear" class="danger">Clear</button><span class="meta" id="meta"></span></div><pre><code id="out" class="language-log"></code></pre></main></div>
<script src="/assets/highlight.min.js"></script>
<script>
const logs=%s, token=%q; let current=%q, raw='', rawMode=false;
const logsEl=document.getElementById('logs'), out=document.getElementById('out'), meta=document.getElementById('meta');
for (const l of logs){const b=document.createElement('button'); b.className='log'; b.textContent=l.label; b.onclick=()=>load(l.id); b.id='log-'+l.id; logsEl.appendChild(b);}
async function load(id){current=id; document.querySelectorAll('.log').forEach(b=>b.classList.toggle('active',b.id==='log-'+id)); raw=await (await fetch('/logs/'+id+'/raw')).text(); render();}
function render(){let lines=raw.split(/\r?\n/); const f=document.getElementById('filter').value.toLowerCase(), lvl=document.getElementById('level').value; if(f)lines=lines.filter(l=>l.toLowerCase().includes(f)); if(lvl)lines=lines.filter(l=>l.toLowerCase().includes(lvl)); if(document.getElementById('order').value==='desc')lines=lines.reverse(); const text=lines.join('\n'); out.textContent=text||'No log lines.'; meta.textContent=lines.length+' lines'; if(!rawMode)hljs.highlightElement(out);}
document.getElementById('filter').oninput=render; document.getElementById('level').onchange=render; document.getElementById('order').onchange=render; document.getElementById('raw').onclick=()=>{rawMode=!rawMode; render();};
document.getElementById('clear').onclick=async()=>{if(!confirm('Clear this log?'))return; await fetch('/logs/'+current+'/clear',{method:'POST',headers:{'X-Herdlite-Token':token}}); await load(current);};
load(current);
</script></body></html>`, string(data), token, first)
}
