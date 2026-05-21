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
	ProjectName string              `json:"project_name"`
	ProjectPath string              `json:"project_path"`
	PHPVersion  string              `json:"php_version"`
	LogID       string              `json:"log_id"`
	Running     bool                `json:"running"`
	PID         int                 `json:"pid"`
	PIDs        []int               `json:"pids"`
	Command     string              `json:"command"`
	Settings    state.QueueSettings `json:"settings"`
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

func (m Manager) Status(project state.Project) WorkerStatus {
	return m.status(project)
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
	settings, err := m.Store.ProjectQueueSettings(project.Name)
	if err != nil {
		logFile.Close()
		return status, err
	}
	args := CommandArgs(settings)
	pids := make([]int, 0, settings.Processes)
	for i := 0; i < settings.Processes; i++ {
		cmd := exec.CommandContext(ctx, runtime.PHPBinary, args...)
		cmd.Dir = project.Path
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		if err := cmd.Start(); err != nil {
			killPIDs(pids)
			logFile.Close()
			return status, err
		}
		pids = append(pids, cmd.Process.Pid)
		if err := cmd.Process.Release(); err != nil {
			killPIDs(pids)
			logFile.Close()
			return status, err
		}
	}
	if err := writePIDs(m.pidPath(project), pids); err != nil {
		killPIDs(pids)
		logFile.Close()
		return status, err
	}
	logFile.Close()

	time.Sleep(100 * time.Millisecond)
	return m.status(project), nil
}

func (m Manager) Stop(project state.Project) (WorkerStatus, error) {
	pids, err := readPIDs(m.pidPath(project))
	if err != nil {
		removeStalePID(m.pidPath(project))
		return m.status(project), nil
	}
	for _, pid := range pids {
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
			return m.status(project), err
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !anyPIDAlive(pids) {
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
	settings, err := m.Store.ProjectQueueSettings(project.Name)
	if err != nil {
		settings = state.DefaultQueueSettings()
	}
	pids, err := readPIDs(m.pidPath(project))
	pids = alivePIDs(pids)
	running := err == nil && len(pids) > 0
	if !running {
		removeStalePID(m.pidPath(project))
		pids = []int{}
	}
	pid := 0
	if len(pids) > 0 {
		pid = pids[0]
	}
	return WorkerStatus{
		ProjectName: project.Name,
		ProjectPath: project.Path,
		PHPVersion:  project.PHPVersion,
		LogID:       "queue-" + IDPart(project.Name),
		Running:     running,
		PID:         pid,
		PIDs:        pids,
		Command:     CommandPreview(settings),
		Settings:    settings,
	}
}

func CommandArgs(settings state.QueueSettings) []string {
	settings = state.NormalizeQueueSettings(settings)
	args := []string{"artisan", "queue:work"}
	if settings.Connection != "" {
		args = append(args, settings.Connection)
	}
	if settings.Queue != "" {
		args = append(args, "--queue="+settings.Queue)
	}
	args = append(args,
		"--backoff="+strconv.Itoa(settings.Backoff),
		"--sleep="+strconv.Itoa(settings.Sleep),
		"--rest="+strconv.Itoa(settings.Rest),
		"--timeout="+strconv.Itoa(settings.Timeout),
		"--tries="+strconv.Itoa(settings.Tries),
		"--memory="+strconv.Itoa(settings.Memory),
	)
	if settings.Env != "" {
		args = append(args, "--env="+settings.Env)
	}
	if settings.Force {
		args = append(args, "--force")
	}
	return args
}

func CommandPreview(settings state.QueueSettings) string {
	return strings.Join(append([]string{"php"}, CommandArgs(settings)...), " ")
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
	pids, err := readPIDs(pidPath)
	if err == nil && !anyPIDAlive(pids) {
		_ = os.Remove(pidPath)
	}
}

func readPID(path string) (int, error) {
	pids, err := readPIDs(path)
	if err != nil {
		return 0, err
	}
	if len(pids) == 0 {
		return 0, fmt.Errorf("no pids in %s", path)
	}
	return pids[0], nil
}

func readPIDs(path string) ([]int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return nil, fmt.Errorf("invalid pid in %s", path)
	}
	pids := make([]int, 0, len(fields))
	for _, field := range fields {
		pid, err := strconv.Atoi(field)
		if err != nil || pid <= 0 {
			return nil, fmt.Errorf("invalid pid in %s", path)
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

func writePIDs(path string, pids []int) error {
	values := make([]string, 0, len(pids))
	for _, pid := range pids {
		values = append(values, strconv.Itoa(pid))
	}
	return os.WriteFile(path, []byte(strings.Join(values, "\n")+"\n"), 0o644)
}

func alivePIDs(pids []int) []int {
	out := make([]int, 0, len(pids))
	for _, pid := range pids {
		if pidAlive(pid) {
			out = append(out, pid)
		}
	}
	return out
}

func anyPIDAlive(pids []int) bool {
	for _, pid := range pids {
		if pidAlive(pid) {
			return true
		}
	}
	return false
}

func killPIDs(pids []int) {
	for _, pid := range pids {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}
