package queue

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

	"herdlite/internal/paths"
	"herdlite/internal/state"
)

type Manager struct {
	Paths paths.Paths
	Store *state.Store
	Out   io.Writer
}

type WorkerStatus struct {
	ProjectName string `json:"project_name"`
	ProjectPath string `json:"project_path"`
	LogID       string `json:"log_id"`
	Running     bool   `json:"running"`
	PID         int    `json:"pid"`
}

func (m Manager) Statuses() ([]WorkerStatus, error) {
	projects, err := m.Store.Projects()
	if err != nil {
		return nil, err
	}
	out := make([]WorkerStatus, 0, len(projects))
	for _, project := range projects {
		status := m.status(project)
		out = append(out, status)
	}
	return out, nil
}

func (m Manager) Start(ctx context.Context, project state.Project) (WorkerStatus, error) {
	status := m.status(project)
	if status.Running {
		return status, nil
	}
	if project.PHPVersion == "" {
		return status, fmt.Errorf("project %s has no PHP version", project.Name)
	}
	runtime, found, err := m.Store.PHPRuntimeForRequest(project.PHPVersion)
	if err != nil {
		return status, err
	}
	if !found {
		return status, fmt.Errorf("PHP %s for project %s is not installed", project.PHPVersion, project.Name)
	}
	artisan := filepath.Join(project.Path, "artisan")
	if _, err := os.Stat(artisan); err != nil {
		return status, fmt.Errorf("artisan not found for project %s", project.Name)
	}
	if err := os.MkdirAll(m.Paths.RuntimeDir, 0o755); err != nil {
		return status, err
	}
	if err := os.MkdirAll(m.Paths.LogDir, 0o755); err != nil {
		return status, err
	}
	removeStalePID(m.pidPath(project))

	logFile, err := os.OpenFile(m.LogPath(project), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return status, err
	}
	cmd := exec.CommandContext(ctx, runtime.PHPBinary, "artisan", "queue:work", "--sleep=3", "--tries=1")
	cmd.Dir = project.Path
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return status, err
	}
	pid := cmd.Process.Pid
	if err := os.WriteFile(m.pidPath(project), []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		_ = cmd.Process.Kill()
		logFile.Close()
		return status, err
	}
	if err := cmd.Process.Release(); err != nil {
		logFile.Close()
		return status, err
	}
	logFile.Close()

	time.Sleep(100 * time.Millisecond)
	return m.status(project), nil
}

func (m Manager) Stop(project state.Project) (WorkerStatus, error) {
	pid, err := readPID(m.pidPath(project))
	if err != nil {
		removeStalePID(m.pidPath(project))
		return m.status(project), nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		return m.status(project), err
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			_ = os.Remove(m.pidPath(project))
			return m.status(project), nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = os.Remove(m.pidPath(project))
	return m.status(project), nil
}

func (m Manager) LogPath(project state.Project) string {
	return filepath.Join(m.Paths.LogDir, "queue-"+IDPart(project.Name)+".log")
}

func (m Manager) pidPath(project state.Project) string {
	return filepath.Join(m.Paths.RuntimeDir, "queue-"+IDPart(project.Name)+".pid")
}

func (m Manager) status(project state.Project) WorkerStatus {
	pid, err := readPID(m.pidPath(project))
	running := err == nil && pidAlive(pid)
	if !running {
		removeStalePID(m.pidPath(project))
		pid = 0
	}
	return WorkerStatus{
		ProjectName: project.Name,
		ProjectPath: project.Path,
		LogID:       "queue-" + IDPart(project.Name),
		Running:     running,
		PID:         pid,
	}
}

func IDPart(value string) string {
	value = strings.ToLower(value)
	var builder strings.Builder
	dashed := false
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			builder.WriteRune(char)
			dashed = false
			continue
		}
		if !dashed {
			builder.WriteByte('-')
			dashed = true
		}
	}
	out := strings.Trim(builder.String(), "-")
	if out == "" {
		return "project"
	}
	return out
}

func pidAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func removeStalePID(pidPath string) {
	pid, err := readPID(pidPath)
	if err == nil && !pidAlive(pid) {
		_ = os.Remove(pidPath)
	}
}

func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid pid in %s", path)
	}
	return pid, nil
}
