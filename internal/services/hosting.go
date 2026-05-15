package services

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"herdlite/internal/daemon"
	"herdlite/internal/nginx"
	"herdlite/internal/paths"
	"herdlite/internal/phpmanager"
	"herdlite/internal/postgres"
	"herdlite/internal/state"
)

type HostingManager struct {
	Paths paths.Paths
	Store *state.Store
	Out   io.Writer
}

func (m HostingManager) Start(ctx context.Context) error {
	projects, err := m.Store.Projects()
	if err != nil {
		return err
	}
	if len(projects) == 0 {
		return fmt.Errorf("no linked projects; run `herdlite link` first")
	}

	needed, err := m.requiredPHPRuntimes(projects)
	if err != nil {
		return err
	}

	if err := (postgres.Manager{Paths: m.Paths, Out: m.Out}).Start(ctx); err != nil {
		return fmt.Errorf("start PostgreSQL: %w", err)
	}

	for _, runtime := range needed {
		if err := m.startFPM(ctx, runtime); err != nil {
			return err
		}
	}

	nginxManager := nginx.Manager{Paths: m.Paths}
	if _, err := nginxManager.WriteBaseConfig(); err != nil {
		return fmt.Errorf("render nginx config: %w", err)
	}
	if _, err := nginxManager.WriteDebugSite(); err != nil {
		return fmt.Errorf("render nginx debug site: %w", err)
	}
	if err := m.startOrReloadNginx(ctx); err != nil {
		return err
	}
	if err := (daemon.Manager{Paths: m.Paths, Store: m.Store, Out: m.Out}).Start(ctx); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	return nil
}

func (m HostingManager) Stop(ctx context.Context) error {
	if err := (daemon.Manager{Paths: m.Paths, Store: m.Store, Out: m.Out}).Stop(); err != nil {
		return fmt.Errorf("stop daemon: %w", err)
	}
	if err := m.stopNginx(ctx); err != nil {
		return err
	}

	runtimes, err := m.Store.PHPRuntimes()
	if err != nil {
		return err
	}
	for _, runtime := range runtimes {
		if err := m.stopFPM(runtime); err != nil {
			return err
		}
	}
	if err := (postgres.Manager{Paths: m.Paths, Out: m.Out}).Stop(ctx); err != nil {
		return fmt.Errorf("stop PostgreSQL: %w", err)
	}
	return nil
}

func (m HostingManager) requiredPHPRuntimes(projects []state.Project) ([]state.PHPRuntime, error) {
	seen := map[string]bool{}
	var out []state.PHPRuntime
	for _, project := range projects {
		if !project.Enabled {
			continue
		}
		requested := project.PHPVersion
		if requested == "" {
			return nil, fmt.Errorf("project %s has no PHP version; relink it with `herdlite link --php <version>`", project.Name)
		}
		runtime, found, err := m.Store.PHPRuntimeForRequest(requested)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("PHP %s for project %s is not installed", requested, project.Name)
		}
		if seen[runtime.Version] {
			continue
		}
		seen[runtime.Version] = true
		out = append(out, runtime)
	}
	return out, nil
}

func (m HostingManager) startFPM(ctx context.Context, runtime state.PHPRuntime) error {
	paths := phpmanager.ConfigPathsForPrefix(runtime.Prefix)
	if _, err := phpmanager.RenderConfigForPaths(runtime.Prefix, m.Paths); err != nil {
		return fmt.Errorf("render PHP-FPM config for %s: %w", runtime.Version, err)
	}
	if pidAlive(paths.PID) {
		m.printf("PHP-FPM %s already running.\n", runtime.Version)
		return nil
	}
	removeStalePID(paths.PID)

	if err := run(ctx, runtime.PHPFPMBinary, "-t", "--fpm-config", paths.PHPFPM); err != nil {
		return fmt.Errorf("validate PHP-FPM %s config: %w", runtime.Version, err)
	}

	if err := os.MkdirAll(m.Paths.LogDir, 0o755); err != nil {
		return err
	}
	logPath := filepath.Join(m.Paths.LogDir, "php-fpm-"+runtime.Version+".launcher.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, runtime.PHPFPMBinary, "--fpm-config", paths.PHPFPM)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start PHP-FPM %s: %w", runtime.Version, err)
	}
	if err := cmd.Process.Release(); err != nil {
		logFile.Close()
		return err
	}
	logFile.Close()

	time.Sleep(250 * time.Millisecond)
	if !pidAlive(paths.PID) {
		return fmt.Errorf("PHP-FPM %s did not start; check %s", runtime.Version, logPath)
	}
	m.printf("Started PHP-FPM %s.\n", runtime.Version)
	return nil
}

func (m HostingManager) startOrReloadNginx(ctx context.Context) error {
	binary, err := exec.LookPath("nginx")
	if err != nil {
		return fmt.Errorf("nginx not found")
	}
	config := filepath.Join(m.Paths.NginxDir, "nginx.conf")
	pid := filepath.Join(m.Paths.NginxDir, "nginx.pid")

	if err := run(ctx, binary, "-t", "-c", config); err != nil {
		return fmt.Errorf("validate nginx config: %w", err)
	}

	if pidAlive(pid) {
		if err := run(ctx, binary, "-s", "reload", "-c", config); err != nil {
			return fmt.Errorf("reload nginx: %w", err)
		}
		m.printf("Reloaded nginx.\n")
		return nil
	}
	removeStalePID(pid)

	if err := run(ctx, binary, "-c", config); err != nil {
		return fmt.Errorf("start nginx: %w", err)
	}
	m.printf("Started nginx.\n")
	return nil
}

func (m HostingManager) stopNginx(ctx context.Context) error {
	binary, err := exec.LookPath("nginx")
	if err != nil {
		return fmt.Errorf("nginx not found")
	}
	config := filepath.Join(m.Paths.NginxDir, "nginx.conf")
	pid := filepath.Join(m.Paths.NginxDir, "nginx.pid")
	if !pidAlive(pid) {
		removeStalePID(pid)
		m.printf("nginx is not running.\n")
		return nil
	}
	if err := run(ctx, binary, "-s", "quit", "-c", config); err != nil {
		return fmt.Errorf("stop nginx: %w", err)
	}
	m.printf("Stopped nginx.\n")
	return nil
}

func (m HostingManager) stopFPM(runtime state.PHPRuntime) error {
	paths := phpmanager.ConfigPathsForPrefix(runtime.Prefix)
	pid, err := readPID(paths.PID)
	if err != nil {
		removeStalePID(paths.PID)
		m.printf("PHP-FPM %s is not running.\n", runtime.Version)
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		if err == syscall.ESRCH {
			removeStalePID(paths.PID)
			m.printf("PHP-FPM %s is not running.\n", runtime.Version)
			return nil
		}
		return fmt.Errorf("stop PHP-FPM %s: %w", runtime.Version, err)
	}
	m.printf("Stopped PHP-FPM %s.\n", runtime.Version)
	return nil
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func pidAlive(pidPath string) bool {
	pid, err := readPID(pidPath)
	if err != nil {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

func removeStalePID(pidPath string) {
	if _, err := readPID(pidPath); err == nil && !pidAlive(pidPath) {
		_ = os.Remove(pidPath)
	}
}

func readPID(pidPath string) (int, error) {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid pid in %s", pidPath)
	}
	return pid, nil
}

func (m HostingManager) printf(format string, args ...any) {
	if m.Out == nil {
		return
	}
	fmt.Fprintf(m.Out, format, args...)
}
