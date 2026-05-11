package mail

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	smtp "github.com/emersion/go-smtp"

	"herdlite/internal/paths"
	"herdlite/internal/state"
)

const (
	SMTPAddr = "127.0.0.1:1025"
	HTTPAddr = "127.0.0.1:7391"
)

type Service struct {
	Paths         paths.Paths
	Store         *state.Store
	Out           io.Writer
	ExtraHandlers func(*http.ServeMux)
}

func (s Service) Run(ctx context.Context) error {
	smtpServer := smtp.NewServer(&backend{store: s.Store})
	smtpServer.Addr = SMTPAddr
	smtpServer.Domain = "herdlite.local"
	smtpServer.ReadTimeout = 10 * time.Second
	smtpServer.WriteTimeout = 10 * time.Second
	smtpServer.MaxMessageBytes = 25 << 20
	smtpServer.MaxRecipients = 100
	smtpServer.AllowInsecureAuth = true
	smtpServer.ErrorLog = log.New(s.Out, "smtp: ", log.LstdFlags)

	httpServer := &http.Server{
		Addr:              HTTPAddr,
		Handler:           s.httpHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errs := make(chan error, 2)
	go func() {
		err := smtpServer.ListenAndServe()
		if err != nil && !errors.Is(err, smtp.ErrServerClosed) {
			errs <- err
			return
		}
		errs <- nil
	}()
	go func() {
		err := httpServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
			return
		}
		errs <- nil
	}()

	select {
	case <-ctx.Done():
	case err := <-errs:
		if err != nil {
			_ = smtpServer.Shutdown(context.Background())
			_ = httpServer.Shutdown(context.Background())
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = smtpServer.Shutdown(shutdownCtx)
	_ = httpServer.Shutdown(shutdownCtx)
	return nil
}

func (s Service) httpHandler() http.Handler {
	mux := http.NewServeMux()
	if s.ExtraHandlers != nil {
		s.ExtraHandlers(mux)
	}
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/mail/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/mail/")
		if strings.HasSuffix(rest, "/html") {
			s.serveMailHTML(w, r, strings.TrimSuffix(rest, "/html"))
			return
		}
		if strings.Contains(rest, "/attachments/") {
			s.serveAttachment(w, r, rest)
			return
		}
		http.NotFound(w, r)
	})
	return mux
}

func (s Service) serveMailHTML(w http.ResponseWriter, r *http.Request, idText string) {
	if idText == "" {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		http.NotFound(w, r)
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src data:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	fmt.Fprint(w, renderMailShell(message))
}

func (s Service) serveAttachment(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.Split(rest, "/")
	if len(parts) != 3 || parts[1] != "attachments" {
		http.NotFound(w, r)
		return
	}
	messageID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	attachmentID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	attachment, found, err := s.Store.MailAttachment(messageID, attachmentID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}

	contentType := attachment.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	filename := attachment.Filename
	if filename == "" {
		filename = fmt.Sprintf("attachment-%d", attachment.ID)
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(attachment.Content)))
	w.Header().Set("Content-Disposition", contentDisposition(filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(attachment.Content)
}

func renderMailShell(message state.MailMessage) string {
	body := message.HTMLBody
	if body == "" {
		body = "<pre>" + html.EscapeString(message.TextBody) + "</pre>"
	}

	var attachments strings.Builder
	if len(message.Attachments) == 0 {
		attachments.WriteString(`<span class="muted">No attachments</span>`)
	} else {
		attachments.WriteString(`<div class="attachment-list">`)
		for _, attachment := range message.Attachments {
			name := attachment.Filename
			if name == "" {
				name = fmt.Sprintf("attachment-%d", attachment.ID)
			}
			fmt.Fprintf(&attachments, `<a class="attachment" href="/mail/%d/attachments/%d">%s <span>%s, %d bytes</span></a>`,
				message.ID,
				attachment.ID,
				html.EscapeString(name),
				html.EscapeString(attachment.ContentType),
				attachment.Size,
			)
		}
		attachments.WriteString(`</div>`)
	}

	return fmt.Sprintf(`<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>%s</title>
<style>
body{margin:0;background:#eef1f4;color:#17202a;font-family:Inter,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
.app{min-height:100vh;display:grid;grid-template-columns:280px 1fr}
.sidebar{background:#18212c;color:#dbe4ee;padding:18px 14px}
.brand{font-size:18px;font-weight:700;margin-bottom:18px}
.folder{padding:10px 12px;border-radius:6px;background:#243244;margin-bottom:8px}
.main{display:flex;flex-direction:column;min-width:0}
.toolbar{height:52px;background:#fff;border-bottom:1px solid #d8dee6;display:flex;align-items:center;gap:8px;padding:0 18px}
.button{border:1px solid #cad2dc;border-radius:6px;padding:7px 10px;color:#3a4654;background:#f8fafc}
.header{background:#fff;padding:22px 28px;border-bottom:1px solid #d8dee6}
h1{font-size:22px;line-height:1.25;margin:0 0 14px}
.meta{display:grid;grid-template-columns:90px 1fr;gap:7px 12px;font-size:14px}
.label{color:#6b7684}
.content{padding:24px 28px}
.paper{background:#fff;border:1px solid #d8dee6;border-radius:8px;overflow:auto;padding:24px;max-width:1100px}
.attachments{margin-top:18px;padding-top:14px;border-top:1px solid #e2e7ee}
.attachment-list{display:flex;flex-wrap:wrap;gap:8px;margin-top:8px}
.attachment{display:inline-flex;gap:8px;text-decoration:none;color:#17202a;border:1px solid #cbd5df;border-radius:6px;padding:8px 10px;background:#f8fafc}
.attachment span{color:#6b7684}
.muted{color:#6b7684}
pre{white-space:pre-wrap;font-family:ui-monospace,SFMono-Regular,Menlo,monospace}
</style>
</head>
<body>
<div class="app">
<aside class="sidebar"><div class="brand">Herdlite Mail</div><div class="folder">Inbox</div><div class="folder">Project: %s</div></aside>
<main class="main">
<div class="toolbar"><span class="button">Reply</span><span class="button">Forward</span><span class="button">Archive</span><span class="button">Delete</span></div>
<section class="header">
<h1>%s</h1>
<div class="meta">
<div class="label">From</div><div>%s</div>
<div class="label">To</div><div>%s</div>
<div class="label">Reply-To</div><div>%s</div>
<div class="label">Received</div><div>%s</div>
</div>
<div class="attachments"><strong>Attachments</strong>%s</div>
</section>
<section class="content"><article class="paper">%s</article></section>
</main>
</div>
</body>
</html>`,
		html.EscapeString(message.Subject),
		html.EscapeString(message.ProjectName),
		html.EscapeString(message.Subject),
		html.EscapeString(message.Sender),
		html.EscapeString(message.Recipients),
		html.EscapeString(message.ReplyTo),
		html.EscapeString(message.ReceivedAt.Local().Format(time.RFC1123)),
		attachments.String(),
		body,
	)
}

func contentDisposition(filename string) string {
	filename = strings.ReplaceAll(filename, `"`, "")
	if filename == "" {
		filename = "attachment"
	}
	return fmt.Sprintf(`attachment; filename="%s"`, filename)
}

type backend struct {
	store *state.Store
}

func (b *backend) NewSession(_ *smtp.Conn) (smtp.Session, error) {
	return &session{store: b.store}, nil
}

type session struct {
	store      *state.Store
	from       string
	recipients []string
}

func (s *session) Mail(from string, _ *smtp.MailOptions) error {
	s.from = from
	s.recipients = nil
	return nil
}

func (s *session) Rcpt(to string, _ *smtp.RcptOptions) error {
	s.recipients = append(s.recipients, to)
	return nil
}

func (s *session) Data(r io.Reader) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	parsed, err := ParseMessage(raw, s.from, s.recipients)
	if err != nil {
		return err
	}
	projects, err := s.store.Projects()
	if err != nil {
		return err
	}
	project := DetectProjectName(parsed, projects)
	_, err = s.store.AddMailMessage(parsed.ToStateMessage(project))
	return err
}

func (s *session) Reset() {
	s.from = ""
	s.recipients = nil
}

func (s *session) Logout() error {
	return nil
}

func PortAvailable(addr string) bool {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	listener.Close()
	return true
}
