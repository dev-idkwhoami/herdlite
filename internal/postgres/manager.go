package postgres

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"herdlite/internal/paths"
)

type Manager struct {
	Paths paths.Paths
	Out   io.Writer
}

func (m Manager) DataDir() string {
	return filepath.Join(m.Paths.PostgresDir, "data")
}

func (m Manager) SocketDir() string {
	return filepath.Join(m.Paths.PostgresDir, "run")
}

func (m Manager) LogPath() string {
	return filepath.Join(m.Paths.LogDir, "postgres.log")
}

func (m Manager) Init(ctx context.Context) error {
	if initialized(m.DataDir()) {
		m.printf("PostgreSQL data directory already initialized.\n")
		m.printf("  data: %s\n", m.DataDir())
		if err := m.Start(ctx); err != nil {
			return err
		}
		if err := m.EnsureRootRole(ctx); err != nil {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(m.DataDir(), 0o700); err != nil {
		return err
	}
	initdb, err := exec.LookPath("initdb")
	if err != nil {
		return fmt.Errorf("initdb not found")
	}
	m.printf("Initializing PostgreSQL data directory...\n")
	if err := m.runQuiet(ctx, initdb, "-D", m.DataDir(), "--encoding=UTF8", "--locale=C", "--username=root"); err != nil {
		return err
	}
	m.printf("PostgreSQL data directory initialized.\n")
	m.printf("  data: %s\n", m.DataDir())
	m.printf("  superuser: root\n")
	return nil
}

func (m Manager) Start(ctx context.Context) error {
	if !initialized(m.DataDir()) {
		if err := m.Init(ctx); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(m.SocketDir(), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(m.Paths.LogDir, 0o755); err != nil {
		return err
	}
	pgCtl, err := exec.LookPath("pg_ctl")
	if err != nil {
		return fmt.Errorf("pg_ctl not found")
	}
	if running, _ := m.isRunning(ctx, pgCtl); running {
		m.printf("PostgreSQL is already running.\n")
		m.printf("  data:   %s\n", m.DataDir())
		m.printf("  socket: %s\n", m.SocketDir())
		return nil
	}
	opts := "-k " + m.SocketDir() + " -h 127.0.0.1"
	m.printf("Starting PostgreSQL...\n")
	if err := m.runQuiet(ctx, pgCtl, "-D", m.DataDir(), "-l", m.LogPath(), "-o", opts, "start"); err != nil {
		return err
	}
	m.printf("PostgreSQL is running.\n")
	m.printf("  data:   %s\n", m.DataDir())
	m.printf("  socket: %s\n", m.SocketDir())
	m.printf("  log:    %s\n", m.LogPath())
	return nil
}

func (m Manager) Stop(ctx context.Context) error {
	pgCtl, err := exec.LookPath("pg_ctl")
	if err != nil {
		return fmt.Errorf("pg_ctl not found")
	}
	if !initialized(m.DataDir()) {
		m.printf("PostgreSQL data directory is not initialized.\n")
		return nil
	}
	if running, _ := m.isRunning(ctx, pgCtl); !running {
		m.printf("PostgreSQL is already stopped.\n")
		return nil
	}
	m.printf("Stopping PostgreSQL...\n")
	if err := m.runQuiet(ctx, pgCtl, "-D", m.DataDir(), "stop", "-m", "fast"); err != nil {
		return err
	}
	m.printf("PostgreSQL stopped.\n")
	return nil
}

func (m Manager) Status(ctx context.Context) error {
	pgCtl, err := exec.LookPath("pg_ctl")
	if err != nil {
		return fmt.Errorf("pg_ctl not found")
	}
	if !initialized(m.DataDir()) {
		m.printf("PostgreSQL data directory is not initialized.\n")
		m.printf("  data: %s\n", m.DataDir())
		return nil
	}
	running, output := m.isRunning(ctx, pgCtl)
	if !running {
		m.printf("PostgreSQL is stopped.\n")
		m.printf("  data: %s\n", m.DataDir())
		return nil
	}
	m.printf("PostgreSQL is running.\n")
	m.printf("  data:   %s\n", m.DataDir())
	m.printf("  socket: %s\n", m.SocketDir())
	if pid := parsePID(output); pid != "" {
		m.printf("  pid:    %s\n", pid)
	}
	return nil
}

func (m Manager) EnsureDatabase(ctx context.Context, name string) error {
	if !validDatabaseName(name) {
		return fmt.Errorf("invalid database name %q", name)
	}
	createdb, err := exec.LookPath("createdb")
	if err != nil {
		return fmt.Errorf("createdb not found")
	}
	if err := m.Start(ctx); err != nil {
		return err
	}

	if exists, err := m.databaseExists(ctx, name); err != nil {
		m.printf("Warning: failed to check PostgreSQL database %s: %v\n", name, err)
	} else if exists {
		m.printf("PostgreSQL database already exists: %s\n", name)
		return nil
	}

	cmd := exec.CommandContext(ctx, createdb, "-h", "127.0.0.1", "-U", "root", "-O", "root", name)
	cmd.Stdout = m.Out
	cmd.Stderr = m.Out
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("create database %s: %w", name, err)
	}
	m.printf("Created PostgreSQL database: %s\n", name)
	return nil
}

func (m Manager) EnsureRootRole(ctx context.Context) error {
	psql, err := exec.LookPath("psql")
	if err != nil {
		return fmt.Errorf("psql not found")
	}
	sql := "DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'root') THEN CREATE ROLE root LOGIN SUPERUSER; END IF; END $$;"
	cmd := exec.CommandContext(ctx, psql, "-h", "127.0.0.1", "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", sql)
	cmd.Stdout = m.Out
	cmd.Stderr = m.Out
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ensure PostgreSQL root role: %w", err)
	}
	m.printf("Ensured PostgreSQL role: root\n")
	return nil
}

func (m Manager) databaseExists(ctx context.Context, name string) (bool, error) {
	psql, err := exec.LookPath("psql")
	if err != nil {
		return false, err
	}
	query := fmt.Sprintf("SELECT 1 FROM pg_database WHERE datname = '%s'", name)
	cmd := exec.CommandContext(ctx, psql, "-h", "127.0.0.1", "-U", "root", "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-tAc", query)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)) == "1", nil
}

func initialized(dataDir string) bool {
	_, err := os.Stat(filepath.Join(dataDir, "PG_VERSION"))
	return err == nil
}

func validDatabaseName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func (m Manager) runQuiet(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %w: %s", filepath.Base(name), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (m Manager) isRunning(ctx context.Context, pgCtl string) (bool, string) {
	cmd := exec.CommandContext(ctx, pgCtl, "-D", m.DataDir(), "status")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true, string(out)
	}
	return false, string(out)
}

func parsePID(output string) string {
	const marker = "PID:"
	index := strings.Index(output, marker)
	if index < 0 {
		return ""
	}
	rest := strings.TrimSpace(output[index+len(marker):])
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(fields[0], ")")
}

func (m Manager) printf(format string, args ...any) {
	if m.Out == nil {
		return
	}
	fmt.Fprintf(m.Out, format, args...)
}
