package services

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"syscall"
	"time"

	"herdlite/internal/daemon"
	"herdlite/internal/phpmanager"
	"herdlite/internal/queue"
	"herdlite/internal/state"
)

// MaintenanceState records actual running processes, not the desired project settings.
// PostgreSQL is left running: replacing PHP does not require stopping the database.
type MaintenanceState struct {
	Nginx  bool
	Daemon bool
	FPM    []state.PHPRuntime
	Queues []state.Project
}

func (m HostingManager) CapturePHPState() (MaintenanceState, error) {
	s := MaintenanceState{Nginx: pidAlive(filepath.Join(m.Paths.NginxDir, "nginx.pid")), Daemon: pidAlive((daemon.Manager{Paths: m.Paths}).PIDPath())}
	runtimes, err := m.Store.PHPRuntimes()
	if err != nil {
		return s, err
	}
	for _, r := range runtimes {
		if pidAlive(phpmanager.ConfigPathsForPrefix(r.Prefix).PID) {
			s.FPM = append(s.FPM, r)
		}
	}
	projects, err := m.Store.Projects()
	if err != nil {
		return s, err
	}
	q := queue.Manager{Paths: m.Paths, Store: m.Store, Out: m.Out}
	for _, p := range projects {
		if q.Status(p).Running {
			s.Queues = append(s.Queues, p)
		}
	}
	return s, nil
}

func (m HostingManager) PausePHP(ctx context.Context, s MaintenanceState) error {
	// Stop the daemon first so its UI cannot launch new workers during maintenance.
	if s.Daemon {
		dm := daemon.Manager{Paths: m.Paths, Store: m.Store, Out: m.Out}
		pid, err := readPID(dm.PIDPath())
		if err != nil {
			return err
		}
		if err := dm.Stop(); err != nil {
			return err
		}
		if err := waitStopped(ctx, []int{pid}); err != nil {
			return fmt.Errorf("daemon: %w", err)
		}
	}
	if s.Nginx {
		pid, err := readPID(filepath.Join(m.Paths.NginxDir, "nginx.pid"))
		if err != nil {
			return err
		}
		if err := m.stopNginx(ctx); err != nil {
			return err
		}
		if err := waitStopped(ctx, []int{pid}); err != nil {
			return fmt.Errorf("nginx: %w", err)
		}
	}
	q := queue.Manager{Paths: m.Paths, Store: m.Store, Out: m.Out}
	for _, p := range s.Queues {
		status := q.Status(p)
		if _, err := q.Stop(p); err != nil {
			return err
		}
		if err := waitStopped(ctx, status.PIDs); err != nil {
			return fmt.Errorf("queue %s: %w", p.Name, err)
		}
	}
	for _, r := range s.FPM {
		pid, err := readPID(phpmanager.ConfigPathsForPrefix(r.Prefix).PID)
		if err != nil {
			return err
		}
		if err := m.stopFPM(r); err != nil {
			return err
		}
		if err := waitStopped(ctx, []int{pid}); err != nil {
			return fmt.Errorf("PHP-FPM %s: %w", r.Version, err)
		}
	}
	return nil
}

func waitStopped(ctx context.Context, pids []int) error {
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		alive := false
		for _, pid := range pids {
			if syscall.Kill(pid, 0) == nil {
				alive = true
			}
		}
		if !alive {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("processes did not stop: %v", pids)
		case <-ticker.C:
		}
	}
}

func (m HostingManager) RestorePHP(ctx context.Context, s MaintenanceState, replacements map[string]state.PHPRuntime) error {
	var errs []error
	seen := map[string]bool{}
	for _, old := range s.FPM {
		r := old
		if replacement, ok := replacements[old.Version]; ok {
			r = replacement
		}
		if seen[r.Version] {
			continue
		}
		seen[r.Version] = true
		if err := m.startConfiguredFPM(ctx, r); err != nil {
			errs = append(errs, err)
		}
	}
	q := queue.Manager{Paths: m.Paths, Store: m.Store, Out: m.Out}
	for _, old := range s.Queues {
		p, found, err := m.Store.ProjectByNameOrDomain(old.Name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !found {
			errs = append(errs, fmt.Errorf("queue project %s disappeared", old.Name))
			continue
		}
		status, err := q.Start(ctx, p)
		if err != nil {
			errs = append(errs, err)
		} else if !status.Running {
			errs = append(errs, fmt.Errorf("queue %s did not start", p.Name))
		}
	}
	if s.Nginx {
		errs = append(errs, m.startOrReloadNginx(ctx))
	}
	if s.Daemon {
		errs = append(errs, (daemon.Manager{Paths: m.Paths, Store: m.Store, Out: m.Out}).Start(ctx))
	}
	return errors.Join(errs...)
}
