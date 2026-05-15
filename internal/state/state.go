package state

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	path string
}

type Project struct {
	Name      string
	Path      string
	Domain    string
	Websocket WebsocketSettings

	PHPVersion string
	NodeMode   string
	Enabled    bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type ProjectOptions struct {
	Name             string
	Domain           string
	PHPVersion       string
	WebsocketEnabled bool
	WebsocketPort    int
}

type WebsocketSettings struct {
	Enabled bool
	Domain  string
	Port    int
}

type PHPRuntime struct {
	Version      string
	Minor        string
	Tag          string
	Source       string
	SourceURL    string
	Prefix       string
	PHPBinary    string
	PHPFPMBinary string
	InstalledAt  time.Time
}

type PHPEOLBranch struct {
	Minor       string
	EOLDate     string
	LastRelease string
	FetchedAt   time.Time
}

type MailMessage struct {
	ID          int64
	ProjectName string
	Sender      string
	ReplyTo     string
	Recipients  string
	Subject     string
	TextBody    string
	HTMLBody    string
	RawMIME     []byte
	ReceivedAt  time.Time
	Attachments []MailAttachment
}

type MailAttachment struct {
	ID          int64
	MessageID   int64
	Filename    string
	ContentType string
	ContentID   string
	Size        int64
	Content     []byte
}

type MailFilter struct {
	ProjectName string
	UnknownOnly bool
	All         bool
}

type DebugDump struct {
	ID          int64
	ProjectName string
	ProjectPath string
	SAPI        string
	URI         string
	Command     string
	File        string
	HTML        string
	CapturedAt  time.Time
}

const UnknownProjectName = "Unknown Project"
const MaxDebugDumps = 1000

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Reset() error {
	err := os.Remove(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Store) AddProject(projectPath string) (Project, error) {
	return s.AddProjectWithOptions(projectPath, ProjectOptions{})
}

func (s *Store) AddProjectWithOptions(projectPath string, opts ProjectOptions) (Project, error) {
	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		return Project{}, err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return Project{}, err
	}
	if !info.IsDir() {
		return Project{}, fmt.Errorf("%s is not a directory", absPath)
	}

	publicIndex := filepath.Join(absPath, "public", "index.php")
	if _, err := os.Stat(publicIndex); err != nil {
		return Project{}, fmt.Errorf("expected Laravel entrypoint at %s: %w", publicIndex, err)
	}

	db, err := s.open()
	if err != nil {
		return Project{}, err
	}
	defer db.Close()

	existing, found, err := projectByPath(db, absPath)
	if err != nil {
		return Project{}, err
	}
	if found {
		changed := false
		if opts.Name != "" {
			name := sanitizeName(opts.Name)
			if existing.Name != name {
				existing.Name = name
				existing.UpdatedAt = time.Now().UTC()
				changed = true
			}
		}
		if opts.Domain != "" {
			domain := normalizeDomain(opts.Domain)
			if existing.Domain != domain {
				existing.Domain = domain
				existing.UpdatedAt = time.Now().UTC()
				changed = true
			}
		} else if opts.Name != "" {
			domain := existing.Name + ".test"
			if existing.Domain != domain {
				existing.Domain = domain
				existing.UpdatedAt = time.Now().UTC()
				changed = true
			}
		}
		if opts.PHPVersion != "" && existing.PHPVersion != opts.PHPVersion {
			existing.PHPVersion = opts.PHPVersion
			existing.UpdatedAt = time.Now().UTC()
			changed = true
		}
		if opts.WebsocketEnabled {
			if !existing.Websocket.Enabled {
				existing.Websocket.Enabled = true
				existing.UpdatedAt = time.Now().UTC()
				changed = true
			}
			if opts.WebsocketPort > 0 {
				existing.Websocket.Port = opts.WebsocketPort
			}
		}
		if existing.Websocket.Enabled {
			wsDomain := "ws." + existing.Domain
			if existing.Websocket.Domain != wsDomain {
				existing.Websocket.Domain = wsDomain
				existing.UpdatedAt = time.Now().UTC()
				changed = true
			}
		}
		if opts.WebsocketEnabled && opts.WebsocketPort > 0 && existing.Websocket.Port != opts.WebsocketPort {
			existing.Websocket.Port = opts.WebsocketPort
			existing.UpdatedAt = time.Now().UTC()
			changed = true
		}
		if changed {
			if err := updateProjectByPath(db, existing); err != nil {
				return Project{}, err
			}
		}
		return existing, nil
	}

	now := time.Now().UTC()
	name := sanitizeName(filepath.Base(absPath))
	if opts.Name != "" {
		name = sanitizeName(opts.Name)
	}
	domain := name + ".test"
	if opts.Domain != "" {
		domain = normalizeDomain(opts.Domain)
	}
	wsPort := opts.WebsocketPort
	if wsPort == 0 {
		wsPort = 8080
	}

	project := Project{
		Name:       name,
		Path:       absPath,
		Domain:     domain,
		PHPVersion: opts.PHPVersion,
		NodeMode:   "nvmrc",
		Websocket: WebsocketSettings{
			Enabled: opts.WebsocketEnabled,
			Domain:  "ws." + domain,
			Port:    wsPort,
		},
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := upsertProject(db, project); err != nil {
		return Project{}, err
	}

	return project, nil
}

func (s *Store) ProjectByPath(projectPath string) (Project, bool, error) {
	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		return Project{}, false, err
	}

	db, err := s.open()
	if err != nil {
		return Project{}, false, err
	}
	defer db.Close()

	return projectByPath(db, absPath)
}

func (s *Store) ProjectByNameOrDomain(value string) (Project, bool, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return Project{}, false, nil
	}
	name := strings.TrimSuffix(value, ".test")

	db, err := s.open()
	if err != nil {
		return Project{}, false, err
	}
	defer db.Close()

	row := db.QueryRow(`
		SELECT name, path, domain, php_version, node_mode, enabled,
		       websocket_enabled, websocket_domain, websocket_port,
		       created_at, updated_at
		FROM projects
		WHERE name = ? OR domain = ?
	`, name, value)

	project, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, false, nil
	}
	if err != nil {
		return Project{}, false, err
	}
	return project, true, nil
}

func (s *Store) ProjectForWorkingDirectory(dir string) (Project, bool, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return Project{}, false, err
	}
	info, err := os.Stat(absDir)
	if err != nil {
		return Project{}, false, err
	}
	if !info.IsDir() {
		absDir = filepath.Dir(absDir)
	}

	projects, err := s.Projects()
	if err != nil {
		return Project{}, false, err
	}

	byPath := map[string]Project{}
	for _, project := range projects {
		byPath[filepath.Clean(project.Path)] = project
	}

	for {
		if project, ok := byPath[filepath.Clean(absDir)]; ok {
			return project, true, nil
		}
		parent := filepath.Dir(absDir)
		if parent == absDir {
			return Project{}, false, nil
		}
		absDir = parent
	}
}

func (s *Store) SetProjectPHPVersion(name string, version string) (Project, error) {
	db, err := s.open()
	if err != nil {
		return Project{}, err
	}
	defer db.Close()

	now := time.Now().UTC()
	result, err := db.Exec(`
		UPDATE projects
		SET php_version = ?, updated_at = ?
		WHERE name = ?
	`, version, formatTime(now), name)
	if err != nil {
		return Project{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Project{}, err
	}
	if affected == 0 {
		return Project{}, fmt.Errorf("project %s not found", name)
	}

	row := db.QueryRow(`
		SELECT name, path, domain, php_version, node_mode, enabled,
		       websocket_enabled, websocket_domain, websocket_port,
		       created_at, updated_at
		FROM projects
		WHERE name = ?
	`, name)
	return scanProject(row)
}

func (s *Store) Projects() ([]Project, error) {
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT name, path, domain, php_version, node_mode, enabled,
		       websocket_enabled, websocket_domain, websocket_port,
		       created_at, updated_at
		FROM projects
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

func (s *Store) PHPRuntimes() ([]PHPRuntime, error) {
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT version, minor, tag, source, source_url, prefix, php_binary, php_fpm_binary, installed_at
		FROM php_runtimes
		ORDER BY version DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runtimes []PHPRuntime
	for rows.Next() {
		var runtime PHPRuntime
		var installedAt string
		if err := rows.Scan(&runtime.Version, &runtime.Minor, &runtime.Tag, &runtime.Source, &runtime.SourceURL, &runtime.Prefix, &runtime.PHPBinary, &runtime.PHPFPMBinary, &installedAt); err != nil {
			return nil, err
		}
		runtime.InstalledAt = parseTime(installedAt)
		runtimes = append(runtimes, runtime)
	}
	sortPHPRuntimes(runtimes)
	return runtimes, rows.Err()
}

func (s *Store) LatestPHPRuntime() (PHPRuntime, bool, error) {
	runtimes, err := s.PHPRuntimes()
	if err != nil {
		return PHPRuntime{}, false, err
	}
	if len(runtimes) == 0 {
		return PHPRuntime{}, false, nil
	}
	return runtimes[0], true, nil
}

func (s *Store) PHPRuntimeForRequest(requested string) (PHPRuntime, bool, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" || requested == "latest" {
		return s.LatestPHPRuntime()
	}

	runtimes, err := s.PHPRuntimes()
	if err != nil {
		return PHPRuntime{}, false, err
	}
	for _, runtime := range runtimes {
		if runtime.Version == requested {
			return runtime, true, nil
		}
	}
	for _, runtime := range runtimes {
		if runtime.Minor == requested {
			return runtime, true, nil
		}
	}
	return PHPRuntime{}, false, nil
}

func (s *Store) UpsertPHPRuntime(runtime PHPRuntime) error {
	db, err := s.open()
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(`
		INSERT INTO php_runtimes (
			version, minor, tag, source, source_url, prefix, php_binary, php_fpm_binary, installed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(version) DO UPDATE SET
			minor = excluded.minor,
			tag = excluded.tag,
			source = excluded.source,
			source_url = excluded.source_url,
			prefix = excluded.prefix,
			php_binary = excluded.php_binary,
			php_fpm_binary = excluded.php_fpm_binary,
			installed_at = excluded.installed_at
	`, runtime.Version, runtime.Minor, runtime.Tag, runtime.Source, runtime.SourceURL, runtime.Prefix, runtime.PHPBinary, runtime.PHPFPMBinary, formatTime(runtime.InstalledAt))
	return err
}

func (s *Store) PHPEOLBranches() ([]PHPEOLBranch, error) {
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT minor, eol_date, last_release, fetched_at FROM php_eol_branches`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var branches []PHPEOLBranch
	for rows.Next() {
		var branch PHPEOLBranch
		var fetchedAt string
		if err := rows.Scan(&branch.Minor, &branch.EOLDate, &branch.LastRelease, &fetchedAt); err != nil {
			return nil, err
		}
		branch.FetchedAt = parseTime(fetchedAt)
		branches = append(branches, branch)
	}
	sortPHPEOL(branches)
	return branches, rows.Err()
}

func (s *Store) ReplacePHPEOLBranches(branches []PHPEOLBranch, syncedAt time.Time) error {
	db, err := s.open()
	if err != nil {
		return err
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM php_eol_branches`); err != nil {
		return err
	}

	for _, branch := range branches {
		if _, err := tx.Exec(`
			INSERT INTO php_eol_branches (minor, eol_date, last_release, fetched_at)
			VALUES (?, ?, ?, ?)
		`, branch.Minor, branch.EOLDate, branch.LastRelease, formatTime(branch.FetchedAt)); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(`
		INSERT INTO meta (key, value) VALUES ('last_eol_sync', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, formatTime(syncedAt)); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) PHPEOLMap() (map[string]PHPEOLBranch, error) {
	branches, err := s.PHPEOLBranches()
	if err != nil {
		return nil, err
	}

	out := map[string]PHPEOLBranch{}
	for _, branch := range branches {
		out[branch.Minor] = branch
	}
	return out, nil
}

func (s *Store) MetaValue(key string) (string, bool, error) {
	db, err := s.open()
	if err != nil {
		return "", false, err
	}
	defer db.Close()

	var value string
	err = db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (s *Store) SetMetaValue(key string, value string) error {
	db, err := s.open()
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(`
		INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}

func (s *Store) DeleteMetaValue(key string) error {
	db, err := s.open()
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(`DELETE FROM meta WHERE key = ?`, key)
	return err
}

func (s *Store) AddMailMessage(message MailMessage) (int64, error) {
	db, err := s.open()
	if err != nil {
		return 0, err
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if message.ProjectName == "" {
		message.ProjectName = UnknownProjectName
	}
	if message.ReceivedAt.IsZero() {
		message.ReceivedAt = time.Now().UTC()
	}

	result, err := tx.Exec(`
		INSERT INTO mail_messages (
			project_name, sender, reply_to, recipients, subject,
			text_body, html_body, raw_mime, received_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, message.ProjectName, message.Sender, message.ReplyTo, message.Recipients, message.Subject, message.TextBody, message.HTMLBody, message.RawMIME, formatTime(message.ReceivedAt))
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	for _, attachment := range message.Attachments {
		size := attachment.Size
		if size == 0 {
			size = int64(len(attachment.Content))
		}
		if _, err := tx.Exec(`
			INSERT INTO mail_attachments (
				message_id, filename, content_type, content_id, size, content
			) VALUES (?, ?, ?, ?, ?, ?)
		`, id, attachment.Filename, attachment.ContentType, attachment.ContentID, size, attachment.Content); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) MailMessages(filter MailFilter) ([]MailMessage, error) {
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	query := `
		SELECT id, project_name, sender, reply_to, recipients, subject,
		       text_body, html_body, raw_mime, received_at
		FROM mail_messages
	`
	args := []any{}
	where := mailWhere(filter, &args)
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY id DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []MailMessage
	for rows.Next() {
		message, err := scanMailMessage(rows)
		if err != nil {
			return nil, err
		}
		attachments, err := mailAttachments(db, message.ID)
		if err != nil {
			return nil, err
		}
		message.Attachments = attachments
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

func (s *Store) MailMessage(id int64) (MailMessage, bool, error) {
	db, err := s.open()
	if err != nil {
		return MailMessage{}, false, err
	}
	defer db.Close()

	row := db.QueryRow(`
		SELECT id, project_name, sender, reply_to, recipients, subject,
		       text_body, html_body, raw_mime, received_at
		FROM mail_messages
		WHERE id = ?
	`, id)
	message, err := scanMailMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return MailMessage{}, false, nil
	}
	if err != nil {
		return MailMessage{}, false, err
	}
	attachments, err := mailAttachments(db, id)
	if err != nil {
		return MailMessage{}, false, err
	}
	message.Attachments = attachments
	return message, true, nil
}

func (s *Store) MailAttachment(messageID int64, attachmentID int64) (MailAttachment, bool, error) {
	db, err := s.open()
	if err != nil {
		return MailAttachment{}, false, err
	}
	defer db.Close()

	row := db.QueryRow(`
		SELECT id, message_id, filename, content_type, content_id, size, content
		FROM mail_attachments
		WHERE message_id = ? AND id = ?
	`, messageID, attachmentID)
	var attachment MailAttachment
	err = row.Scan(&attachment.ID, &attachment.MessageID, &attachment.Filename, &attachment.ContentType, &attachment.ContentID, &attachment.Size, &attachment.Content)
	if errors.Is(err, sql.ErrNoRows) {
		return MailAttachment{}, false, nil
	}
	if err != nil {
		return MailAttachment{}, false, err
	}
	return attachment, true, nil
}

func (s *Store) ClearMailMessages(filter MailFilter) (int64, error) {
	db, err := s.open()
	if err != nil {
		return 0, err
	}
	defer db.Close()

	args := []any{}
	where := mailWhere(filter, &args)
	if where == "" {
		where = "1 = 1"
	}

	result, err := db.Exec(`DELETE FROM mail_messages WHERE `+where, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) DeleteMailMessage(id int64) (int64, error) {
	db, err := s.open()
	if err != nil {
		return 0, err
	}
	defer db.Close()

	result, err := db.Exec(`DELETE FROM mail_messages WHERE id = ?`, id)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) AddDebugDump(dump DebugDump) (int64, error) {
	db, err := s.open()
	if err != nil {
		return 0, err
	}
	defer db.Close()

	if dump.ProjectName == "" {
		dump.ProjectName = UnknownProjectName
	}
	if dump.CapturedAt.IsZero() {
		dump.CapturedAt = time.Now().UTC()
	}

	result, err := db.Exec(`
		INSERT INTO debug_dumps (
			project_name, project_path, sapi, uri, command, file, html, captured_at
		)
		SELECT ?, ?, ?, ?, ?, ?, ?, ?
		WHERE (SELECT COUNT(*) FROM debug_dumps) < ?
	`, dump.ProjectName, dump.ProjectPath, dump.SAPI, dump.URI, dump.Command, dump.File, dump.HTML, formatTime(dump.CapturedAt), MaxDebugDumps)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if rows == 0 {
		return 0, nil
	}
	return result.LastInsertId()
}

func (s *Store) DebugDumps(limit int) ([]DebugDump, error) {
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	if limit <= 0 || limit > MaxDebugDumps {
		limit = 100
	}
	rows, err := db.Query(`
		SELECT id, project_name, project_path, sapi, uri, command, file, html, captured_at
		FROM debug_dumps
		ORDER BY id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dumps []DebugDump
	for rows.Next() {
		dump, err := scanDebugDump(rows)
		if err != nil {
			return nil, err
		}
		dumps = append(dumps, dump)
	}
	return dumps, rows.Err()
}

func (s *Store) ClearDebugDumps() (int64, error) {
	db, err := s.open()
	if err != nil {
		return 0, err
	}
	defer db.Close()

	result, err := db.Exec(`DELETE FROM debug_dumps`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) DeleteDebugDump(id int64) (int64, error) {
	db, err := s.open()
	if err != nil {
		return 0, err
	}
	defer db.Close()

	result, err := db.Exec(`DELETE FROM debug_dumps WHERE id = ?`, id)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) ClearDebugDumpsBefore(id int64) (int64, error) {
	db, err := s.open()
	if err != nil {
		return 0, err
	}
	defer db.Close()

	result, err := db.Exec(`DELETE FROM debug_dumps WHERE id < ?`, id)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) open() (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", s.path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func ensureSchema(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS projects (
			name TEXT PRIMARY KEY,
			path TEXT NOT NULL UNIQUE,
			domain TEXT NOT NULL UNIQUE,
			php_version TEXT NOT NULL DEFAULT '',
			node_mode TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			websocket_enabled INTEGER NOT NULL DEFAULT 0,
			websocket_domain TEXT NOT NULL DEFAULT '',
			websocket_port INTEGER NOT NULL DEFAULT 8080,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS php_runtimes (
			version TEXT PRIMARY KEY,
			minor TEXT NOT NULL,
			tag TEXT NOT NULL,
			source TEXT NOT NULL,
			source_url TEXT NOT NULL,
			prefix TEXT NOT NULL,
			php_binary TEXT NOT NULL,
			php_fpm_binary TEXT NOT NULL,
			installed_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS php_eol_branches (
			minor TEXT PRIMARY KEY,
			eol_date TEXT NOT NULL,
			last_release TEXT NOT NULL,
			fetched_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS mail_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_name TEXT NOT NULL,
			sender TEXT NOT NULL,
			reply_to TEXT NOT NULL,
			recipients TEXT NOT NULL,
			subject TEXT NOT NULL,
			text_body TEXT NOT NULL,
			html_body TEXT NOT NULL,
			raw_mime BLOB NOT NULL,
			received_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS mail_attachments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id INTEGER NOT NULL,
			filename TEXT NOT NULL,
			content_type TEXT NOT NULL,
			content_id TEXT NOT NULL,
			size INTEGER NOT NULL,
			content BLOB NOT NULL,
			FOREIGN KEY(message_id) REFERENCES mail_messages(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS debug_dumps (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_name TEXT NOT NULL,
			project_path TEXT NOT NULL,
			sapi TEXT NOT NULL,
			uri TEXT NOT NULL,
			command TEXT NOT NULL,
			file TEXT NOT NULL,
			html TEXT NOT NULL,
			captured_at TEXT NOT NULL
		)`,
	}

	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func mailWhere(filter MailFilter, args *[]any) string {
	if filter.All {
		return ""
	}
	if filter.UnknownOnly {
		*args = append(*args, UnknownProjectName)
		return "project_name = ?"
	}
	if filter.ProjectName != "" {
		*args = append(*args, filter.ProjectName)
		return "project_name = ?"
	}
	return ""
}

func scanDebugDump(scanner mailScanner) (DebugDump, error) {
	var dump DebugDump
	var capturedAt string
	err := scanner.Scan(
		&dump.ID,
		&dump.ProjectName,
		&dump.ProjectPath,
		&dump.SAPI,
		&dump.URI,
		&dump.Command,
		&dump.File,
		&dump.HTML,
		&capturedAt,
	)
	if err != nil {
		return DebugDump{}, err
	}
	dump.CapturedAt = parseTime(capturedAt)
	return dump, nil
}

type mailScanner interface {
	Scan(dest ...any) error
}

func scanMailMessage(scanner mailScanner) (MailMessage, error) {
	var message MailMessage
	var receivedAt string
	err := scanner.Scan(
		&message.ID,
		&message.ProjectName,
		&message.Sender,
		&message.ReplyTo,
		&message.Recipients,
		&message.Subject,
		&message.TextBody,
		&message.HTMLBody,
		&message.RawMIME,
		&receivedAt,
	)
	if err != nil {
		return MailMessage{}, err
	}
	message.ReceivedAt = parseTime(receivedAt)
	return message, nil
}

func mailAttachments(db *sql.DB, messageID int64) ([]MailAttachment, error) {
	rows, err := db.Query(`
		SELECT id, message_id, filename, content_type, content_id, size, content
		FROM mail_attachments
		WHERE message_id = ?
		ORDER BY id
	`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attachments []MailAttachment
	for rows.Next() {
		var attachment MailAttachment
		if err := rows.Scan(&attachment.ID, &attachment.MessageID, &attachment.Filename, &attachment.ContentType, &attachment.ContentID, &attachment.Size, &attachment.Content); err != nil {
			return nil, err
		}
		attachments = append(attachments, attachment)
	}
	return attachments, rows.Err()
}

func projectByPath(db *sql.DB, path string) (Project, bool, error) {
	row := db.QueryRow(`
		SELECT name, path, domain, php_version, node_mode, enabled,
		       websocket_enabled, websocket_domain, websocket_port,
		       created_at, updated_at
		FROM projects
		WHERE path = ?
	`, path)

	project, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, false, nil
	}
	if err != nil {
		return Project{}, false, err
	}
	return project, true, nil
}

func upsertProject(db *sql.DB, project Project) error {
	_, err := db.Exec(`
		INSERT INTO projects (
			name, path, domain, php_version, node_mode, enabled,
			websocket_enabled, websocket_domain, websocket_port,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			path = excluded.path,
			domain = excluded.domain,
			php_version = excluded.php_version,
			node_mode = excluded.node_mode,
			enabled = excluded.enabled,
			websocket_enabled = excluded.websocket_enabled,
			websocket_domain = excluded.websocket_domain,
			websocket_port = excluded.websocket_port,
			updated_at = excluded.updated_at
	`, project.Name, project.Path, project.Domain, project.PHPVersion, project.NodeMode, boolInt(project.Enabled), boolInt(project.Websocket.Enabled), project.Websocket.Domain, project.Websocket.Port, formatTime(project.CreatedAt), formatTime(project.UpdatedAt))
	return err
}

func updateProjectByPath(db *sql.DB, project Project) error {
	_, err := db.Exec(`
		UPDATE projects SET
			name = ?,
			domain = ?,
			php_version = ?,
			node_mode = ?,
			enabled = ?,
			websocket_enabled = ?,
			websocket_domain = ?,
			websocket_port = ?,
			updated_at = ?
		WHERE path = ?
	`, project.Name, project.Domain, project.PHPVersion, project.NodeMode, boolInt(project.Enabled), boolInt(project.Websocket.Enabled), project.Websocket.Domain, project.Websocket.Port, formatTime(project.UpdatedAt), project.Path)
	return err
}

type projectScanner interface {
	Scan(dest ...any) error
}

func scanProject(scanner projectScanner) (Project, error) {
	var project Project
	var enabled int
	var wsEnabled int
	var createdAt string
	var updatedAt string
	err := scanner.Scan(
		&project.Name,
		&project.Path,
		&project.Domain,
		&project.PHPVersion,
		&project.NodeMode,
		&enabled,
		&wsEnabled,
		&project.Websocket.Domain,
		&project.Websocket.Port,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return Project{}, err
	}
	project.Enabled = enabled == 1
	project.Websocket.Enabled = wsEnabled == 1
	project.CreatedAt = parseTime(createdAt)
	project.UpdatedAt = parseTime(updatedAt)
	return project, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		value = time.Now().UTC()
	}
	return value.UTC().Format(time.RFC3339)
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func sortPHPRuntimes(runtimes []PHPRuntime) {
	sort.Slice(runtimes, func(i, j int) bool {
		return compareVersion(runtimes[i].Version, runtimes[j].Version) > 0
	})
}

func sortPHPEOL(branches []PHPEOLBranch) {
	sort.Slice(branches, func(i, j int) bool {
		return compareVersion(branches[i].Minor+".0", branches[j].Minor+".0") > 0
	})
}

func compareVersion(a string, b string) int {
	ap := parseVersionParts(a)
	bp := parseVersionParts(b)
	for i := 0; i < len(ap) && i < len(bp); i++ {
		if ap[i] > bp[i] {
			return 1
		}
		if ap[i] < bp[i] {
			return -1
		}
	}
	return 0
}

func parseVersionParts(version string) [3]int {
	var out [3]int
	part := 0
	value := 0
	for _, r := range version {
		if r >= '0' && r <= '9' {
			value = value*10 + int(r-'0')
			continue
		}
		if r == '.' && part < len(out) {
			out[part] = value
			part++
			value = 0
		}
	}
	if part < len(out) {
		out[part] = value
	}
	return out
}

func sanitizeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	lastDash := false

	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == ' ' || r == '.':
			if !lastDash && b.Len() > 0 {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}

	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "app"
	}
	return out
}

func normalizeDomain(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.Trim(domain, "/")
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return "app.test"
	}
	if !strings.Contains(domain, ".") {
		domain += ".test"
	}
	return domain
}
