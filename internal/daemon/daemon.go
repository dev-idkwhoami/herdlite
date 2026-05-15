package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"herdlite/internal/debugui"
	"herdlite/internal/mail"
	"herdlite/internal/paths"
	"herdlite/internal/state"
)

type Manager struct {
	Paths paths.Paths
	Store *state.Store
	Out   io.Writer
}

type Service struct {
	Paths  paths.Paths
	Store  *state.Store
	Out    io.Writer
	Token  string
	Events *EventHub
}

type Status struct {
	Running bool
	PID     int
	Healthy bool
}

func (m Manager) Start(ctx context.Context) error {
	status := m.Status()
	if status.Running {
		if !status.Healthy {
			m.printf("Daemon process %d is running but health check failed; restarting it.\n", status.PID)
			if err := m.Stop(); err != nil {
				return fmt.Errorf("stop unhealthy daemon: %w", err)
			}
			return m.Start(ctx)
		}
		m.printf("Daemon is already running.\n")
		m.printf("  pid:  %d\n", status.PID)
		m.printf("  mail:  http://%s\n", mail.HTTPAddr)
		m.printf("  debug: %s\n", debugui.BaseURL)
		return nil
	}
	removeStalePID(m.PIDPath())

	if err := os.MkdirAll(m.Paths.RuntimeDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(m.Paths.LogDir, 0o755); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	logPath := m.LogPath()
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, executable, "daemon", "run")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return err
	}
	if err := cmd.Process.Release(); err != nil {
		logFile.Close()
		return err
	}
	logFile.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := m.Status()
		if status.Running && status.Healthy {
			m.printf("Started Herdlite daemon.\n")
			m.printf("  pid:  %d\n", status.PID)
			m.printf("  mail:  http://%s\n", mail.HTTPAddr)
			m.printf("  debug: %s\n", debugui.BaseURL)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not start; check %s", logPath)
}

func (m Manager) Stop() error {
	pid, err := readPID(m.PIDPath())
	if err != nil {
		removeStalePID(m.PIDPath())
		m.printf("Daemon is not running.\n")
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		if err == syscall.ESRCH {
			removeStalePID(m.PIDPath())
			m.printf("Daemon is not running.\n")
			return nil
		}
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !pidExists(pid) {
			_ = os.Remove(m.PIDPath())
			m.printf("Stopped Herdlite daemon.\n")
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = os.Remove(m.PIDPath())
	m.printf("Stopped Herdlite daemon.\n")
	return nil
}

func (m Manager) Status() Status {
	pid, err := readPID(m.PIDPath())
	if err != nil || !pidExists(pid) {
		removeStalePID(m.PIDPath())
		return Status{}
	}
	return Status{Running: true, PID: pid, Healthy: HTTPHealthy()}
}

func (m Manager) PIDPath() string {
	return filepath.Join(m.Paths.RuntimeDir, "herdlite.pid")
}

func (m Manager) LogPath() string {
	return filepath.Join(m.Paths.LogDir, "daemon.log")
}

func (s Service) Run(ctx context.Context) error {
	if err := os.MkdirAll(s.Paths.RuntimeDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile((Manager{Paths: s.Paths}).PIDPath(), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		return err
	}
	defer os.Remove((Manager{Paths: s.Paths}).PIDPath())

	fmt.Fprintln(s.Out, "Herdlite daemon started.")
	fmt.Fprintf(s.Out, "Mail SMTP: %s\n", mail.SMTPAddr)
	fmt.Fprintf(s.Out, "Mail HTTP: http://%s\n", mail.HTTPAddr)
	fmt.Fprintf(s.Out, "Debug UI: %s\n", debugui.BaseURL)
	if s.Token == "" {
		s.Token = randomToken()
	}
	if s.Events == nil {
		s.Events = NewEventHub()
	}
	return (mail.Service{
		Paths:         s.Paths,
		Store:         s.Store,
		Out:           s.Out,
		ExtraHandlers: s.registerHTTPHandlers,
		OnMail: func(message state.MailMessage) {
			s.publish("mail.created", strconv.FormatInt(message.ID, 10))
		},
	}).Run(ctx)
}

func (s Service) registerHTTPHandlers(mux *http.ServeMux) {
	s.registerUIHandlers(mux)
	s.registerLogHandlers(mux)
	s.registerDumpHandlers(mux)
	s.registerMailAPIHandlers(mux)
	s.registerEventHandlers(mux)
	s.registerSessionHandlers(mux)
}

func (s Service) publish(eventType string, id string) {
	if s.Events == nil {
		return
	}
	s.Events.Publish(eventMessage{Type: eventType, ID: id})
}

func HTTPHealthy() bool {
	client := http.Client{Timeout: 200 * time.Millisecond}
	resp, err := client.Get("http://" + mail.HTTPAddr + "/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusNoContent
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

func pidExists(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func removeStalePID(path string) {
	pid, err := readPID(path)
	if err == nil && !pidExists(pid) {
		_ = os.Remove(path)
	}
}

func (m Manager) printf(format string, args ...any) {
	if m.Out == nil {
		return
	}
	fmt.Fprintf(m.Out, format, args...)
}

func randomToken() string {
	var data [24]byte
	if _, err := rand.Read(data[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(data[:])
}
