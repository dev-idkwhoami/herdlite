package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"herdlite/internal/app"
	"herdlite/internal/composer"
	"herdlite/internal/daemon"
	"herdlite/internal/mail"
	"herdlite/internal/postgres"
)

func runDoctor(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite doctor")
		return 0
	}

	heading(a.Out, "System")
	for _, name := range []string{"pacman", "sudo", "nginx", "dnsmasq", "initdb", "pg_ctl", "postgres", "psql", "createdb", "certutil", "trust", "xdg-open", "zsh"} {
		checkBinary(a, name)
	}

	heading(a.Out, "Paths")
	printPaths(a)

	heading(a.Out, "Herdlite")
	checkPath(a, "state", a.Paths.StateFile)
	checkPath(a, "CA", filepath.Join(a.Paths.CADir, "herdlite-local-ca.crt"))
	checkPath(a, "nginx config", filepath.Join(a.Paths.NginxDir, "nginx.conf"))
	checkPath(a, "composer", (composer.Manager{Paths: a.Paths}).Path())
	checkPath(a, "zsh hook", filepath.Join(a.Paths.ConfigDir, "shell", "zsh.zsh"))
	checkPath(a, "NSS DB", filepath.Join(os.Getenv("HOME"), ".pki", "nssdb", "cert9.db"))

	heading(a.Out, "Services")
	status := (daemon.Manager{Paths: a.Paths, Store: a.Store, Out: a.Out}).Status()
	if status.Running && status.Healthy {
		checkLine(a, "daemon", "ok", fmt.Sprintf("pid %d, http://%s", status.PID, mail.HTTPAddr))
	} else if status.Running {
		checkLine(a, "daemon", "warn", fmt.Sprintf("pid %d, health failed", status.PID))
	} else {
		checkLine(a, "daemon", "warn", "stopped")
	}
	if mail.PortAvailable(mail.SMTPAddr) {
		checkLine(a, "mail smtp", "warn", mail.SMTPAddr+" not listening")
	} else {
		checkLine(a, "mail smtp", "ok", mail.SMTPAddr+" in use")
	}
	checkPostgres(a)

	heading(a.Out, "State")
	if _, err := os.Stat(a.Paths.StateFile); err != nil {
		checkLine(a, "projects", "warn", "state database not created")
		checkLine(a, "PHP", "warn", "state database not created")
		return 0
	}
	projects, err := a.Store.Projects()
	if err != nil {
		checkLine(a, "projects", "fail", err.Error())
	} else {
		checkLine(a, "projects", "ok", fmt.Sprintf("%d linked", len(projects)))
	}
	runtimes, err := a.Store.PHPRuntimes()
	if err != nil {
		checkLine(a, "PHP", "fail", err.Error())
	} else {
		checkLine(a, "PHP", "ok", fmt.Sprintf("%d runtimes", len(runtimes)))
	}
	return 0
}

func checkBinary(a *app.App, name string) {
	path, err := exec.LookPath(name)
	if err != nil {
		checkLine(a, name, "fail", "missing")
		return
	}
	checkLine(a, name, "ok", path)
}

func checkPath(a *app.App, label string, path string) {
	if _, err := os.Stat(path); err != nil {
		checkLine(a, label, "warn", "missing "+path)
		return
	}
	checkLine(a, label, "ok", path)
}

func checkPostgres(a *app.App) {
	manager := postgres.Manager{Paths: a.Paths}
	if _, err := os.Stat(manager.DataDir()); err != nil {
		checkLine(a, "postgres", "warn", "not initialized")
		return
	}
	if err := manager.Status(context.Background()); err != nil {
		checkLine(a, "postgres", "warn", err.Error())
		return
	}
	checkLine(a, "postgres", "ok", manager.DataDir())
}

func checkLine(a *app.App, label string, status string, detail string) {
	fmt.Fprintf(a.Out, "  %-14s %-5s %s\n", label, statusText(a.Out, status), dim(a.Out, detail))
}
