package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"herdlite/internal/app"
	"herdlite/internal/buildinfo"
	"herdlite/internal/composer"
	"herdlite/internal/daemon"
	"herdlite/internal/postgres"
	"herdlite/internal/services"
	"herdlite/internal/updater"
)

var updateComponents = []string{"system", "php", "composer", "node", "self"}

func runUpdate(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite update|upgrade [all|php|nginx|postgres|dns|system|composer|node|self] [--dry-run]\nUpdates installed components only. System packages use the existing pacman database, with required dependencies; no full system upgrade.\nPHP-specific options: herdlite php update --help")
		return 0
	}
	component := "all"
	selected := false
	dry := false
	for _, arg := range args {
		if arg == "--dry-run" {
			dry = true
			continue
		}
		if strings.HasPrefix(arg, "-") || selected {
			fmt.Fprintf(a.Err, "unknown update argument: %s\n", arg)
			return 1
		}
		component = arg
		selected = true
	}
	valid := component == "all" || component == "nginx" || component == "postgres" || component == "dns"
	for _, c := range updateComponents {
		valid = valid || component == c
	}
	if !valid {
		fmt.Fprintf(a.Err, "unknown update component: %s\n", component)
		return 1
	}
	components := []string{component}
	if component == "all" {
		components = updateComponents
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	code := 0
	for _, c := range components {
		if ctx.Err() != nil {
			return 1
		}
		fmt.Fprintf(a.Out, "\n== %s update ==\n", c)
		var err error
		if c == "php" {
			phpArgs := []string{}
			if dry {
				phpArgs = append(phpArgs, "--dry-run")
			}
			if runPHPUpdate(a, phpArgs) != 0 {
				code = 1
			}
			continue
		}
		switch c {
		case "system", "nginx", "postgres", "dns":
			err = updatePackages(ctx, a, c, dry)
		case "composer":
			err = updateComposer(ctx, a, dry)
		case "node":
			err = updateNode(ctx, a, dry)
		case "self":
			err = updateSelf(ctx, a, dry)
		}
		if err != nil {
			fmt.Fprintf(a.Err, "%s update: %v\n", c, err)
			code = 1
			// A failed package transaction/restoration may affect all following tools.
			if c == "system" || ctx.Err() != nil {
				return code
			}
		}
	}
	return code
}
func componentUpdate(component string) func(*app.App, []string) int {
	return func(a *app.App, args []string) int {
		if len(args) == 0 || hasHelp(args) {
			fmt.Fprintf(a.Out, "Usage: herdlite %s <update|upgrade> [--dry-run]\n", component)
			return 0
		}
		if args[0] != "update" && args[0] != "upgrade" {
			fmt.Fprintf(a.Err, "unknown %s command: %s\n", component, args[0])
			return 1
		}
		if hasHelp(args[1:]) {
			fmt.Fprintf(a.Out, "Usage: herdlite %s update [--dry-run]\n", component)
			return 0
		}
		return runUpdate(a, append([]string{component}, args[1:]...))
	}
}
func confirmUpdate(ctx context.Context, a *app.App, question string) error {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return fmt.Errorf("requires an interactive terminal; use --dry-run to preview")
	}
	if !updatePrompt(ctx, a.Out, os.Stdin)(question) {
		return fmt.Errorf("update cancelled")
	}
	return nil
}
func updateCommand(ctx context.Context, a *app.App, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = a.Out
	cmd.Stderr = a.Err
	return cmd.Run()
}
func updateComposer(ctx context.Context, a *app.App, dry bool) error {
	m := composer.Manager{Paths: a.Paths, Out: a.Out}
	if !m.Exists() {
		fmt.Fprintln(a.Out, "Composer is not installed; skipped.")
		return nil
	}
	if dry {
		fmt.Fprintln(a.Out, "Would download and validate latest stable Composer, then atomically replace the managed PHAR.")
		return nil
	}
	r, found, err := a.Store.LatestPHPRuntime()
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no managed PHP runtime available")
	}
	if err = confirmUpdate(ctx, a, "Update Composer to latest stable?"); err != nil {
		return err
	}
	return m.Install(ctx, r, false)
}
func updateNode(ctx context.Context, a *app.App, dry bool) error {
	dir, err := updater.NVMDir()
	if err != nil {
		return err
	}
	versions, err := updater.NodeSources(dir)
	if err != nil {
		return err
	}
	if len(versions) == 0 {
		fmt.Fprintln(a.Out, "No nvm-managed Node versions installed; skipped.")
		return nil
	}
	for _, v := range versions {
		fmt.Fprintf(a.Out, "Node %s -> latest in the same major (bundled npm/npx included); retain old versions and exact pins.\n", v)
	}
	if dry {
		return nil
	}
	if err = confirmUpdate(ctx, a, "Update installed Node majors through nvm?"); err != nil {
		return err
	}
	return updater.UpdateNode(ctx, dir, versions, a.Out, a.Err)
}
func updateSelf(ctx context.Context, a *app.App, dry bool) (err error) {
	destination, err := os.Executable()
	if err != nil {
		return err
	}
	destination, err = filepath.EvalSymlinks(destination)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "Herdlite %s -> latest published release at %s (includes daemon, UI, mail, and queue management).\n", buildinfo.Version, destination)
	if dry {
		return nil
	}
	if err = confirmUpdate(ctx, a, "Replace this Herdlite executable with the checksum-verified latest release?"); err != nil {
		return err
	}
	staged, version, err := updater.StageSelf(ctx, destination, buildinfo.Version)
	if err != nil {
		return err
	}
	if staged == "" {
		fmt.Fprintln(a.Out, "Herdlite is already current.")
		return nil
	}
	defer os.Remove(staged)
	dm := daemon.Manager{Paths: a.Paths, Store: a.Store, Out: a.Out}
	running := processPIDFileAlive(dm.PIDPath())
	// Keep a same-filesystem backup until the daemon has restarted successfully.
	backup := staged + ".previous"
	if err = os.Link(destination, backup); err != nil {
		return err
	}
	keepBackup := false
	defer func() {
		if !keepBackup {
			_ = os.Remove(backup)
		}
	}()
	if running {
		if err = dm.Stop(); err != nil {
			return err
		}
	}
	if err = os.Rename(staged, destination); err != nil {
		if running {
			err = errors.Join(err, dm.Start(context.Background()))
		}
		return err
	}
	if running {
		// os.Executable() on Linux can retain the old inode after replacement. Start
		// the daemon explicitly through the new destination path.
		if err = updateCommand(ctx, a, destination, "daemon", "start"); err != nil {
			if stopErr := dm.Stop(); stopErr != nil {
				keepBackup = true
				return errors.Join(err, stopErr, fmt.Errorf("previous executable retained at %s", backup))
			}
			rollback := os.Rename(backup, destination)
			if rollback != nil {
				keepBackup = true
				return errors.Join(err, fmt.Errorf("restore old executable: %w (backup %s)", rollback, backup))
			}
			return errors.Join(err, updateCommand(context.Background(), a, destination, "daemon", "start"))
		}
	}
	fmt.Fprintf(a.Out, "Updated Herdlite to %s.\n", version)
	return nil
}
func processPIDFileAlive(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var pid int
	if _, err = fmt.Sscan(string(data), &pid); err != nil || pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}
func updatePackages(ctx context.Context, a *app.App, component string, dry bool) (err error) {
	targets, err := updater.PackageTargets(component)
	if err != nil {
		return err
	}
	if _, err = exec.LookPath("pacman"); err != nil {
		fmt.Fprintln(a.Out, "pacman is not available; system package updates require Arch Linux.")
		return err
	}
	var installed []string
	for _, pkg := range targets {
		cmd := exec.CommandContext(ctx, "pacman", "-Qq", pkg)
		if cmd.Run() == nil {
			installed = append(installed, pkg)
		} else if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	// pgvector is supplied by AUR on supported installations.
	vector := component == "postgres" || component == "system"
	vector = vector && exec.CommandContext(ctx, "pacman", "-Qq", "pgvector").Run() == nil
	if len(installed) == 0 && !vector {
		fmt.Fprintln(a.Out, "No installed packages for this component; skipped.")
		return nil
	}
	fmt.Fprintf(a.Out, "Update only: %s (plus required dependencies). Uses existing pacman databases; no refresh or full system upgrade.\n", strings.Join(installed, ", "))
	args := append([]string{"-S", "--needed"}, installed...)
	var plan []byte
	if len(installed) > 0 {
		planArgs := append([]string{"-Sp", "--needed", "--print-format", "%n %v"}, installed...)
		plan, err = exec.CommandContext(ctx, "pacman", planArgs...).Output()
		if err != nil {
			return fmt.Errorf("resolve package transaction: %w", err)
		}
		fmt.Fprint(a.Out, string(plan))
		cluster, e := os.ReadFile(filepath.Join(a.Paths.PostgresDir, "data", "PG_VERSION"))
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if err = updater.CheckPostgresTransaction(string(plan), string(cluster)); err != nil {
			return err
		}
	}
	if vector {
		fmt.Fprintln(a.Out, "Update installed pgvector using yay -S --needed pgvector (no system upgrade).")
	}
	if dry {
		fmt.Fprintln(a.Out, "Dry run: no packages or services changed; repository metadata may be stale.")
		return nil
	}
	if len(strings.TrimSpace(string(plan))) == 0 && !vector {
		fmt.Fprintln(a.Out, "Packages are current according to the existing repository metadata.")
		return nil
	}
	if vector {
		if _, err = exec.LookPath("yay"); err != nil {
			return fmt.Errorf("yay is required to update installed pgvector: %w", err)
		}
	}
	if err = confirmUpdate(ctx, a, "Apply these package updates and restore running Herdlite services?"); err != nil {
		return err
	}
	m := services.HostingManager{Paths: a.Paths, Store: a.Store, Out: a.Out}
	previous := services.MaintenanceState{}
	if _, e := os.Stat(a.Paths.StateFile); e == nil {
		previous, err = m.CapturePHPState()
		if err != nil {
			return err
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	pg := postgres.Manager{Paths: a.Paths, Out: a.Out}
	pgRunning := (vector || containsPackage(string(plan), "postgresql") || containsPackage(string(plan), "postgresql-libs")) && processPIDFileAlive(filepath.Join(pg.DataDir(), "postmaster.pid"))
	defer func() {
		restoreCtx := context.Background()
		if pgRunning {
			err = errors.Join(err, pg.Start(restoreCtx))
		}
		err = errors.Join(err, m.RestorePHP(restoreCtx, previous, nil))
	}()
	if err = m.PausePHP(ctx, previous); err != nil {
		return err
	}
	if pgRunning {
		if err = pg.Stop(ctx); err != nil {
			return err
		}
	}
	if len(installed) > 0 {
		if err = updateCommand(ctx, a, "sudo", append([]string{"pacman"}, args...)...); err != nil {
			return err
		}
	}
	if containsPackage(string(plan), "nginx") {
		nginx, e := exec.LookPath("nginx")
		if e != nil {
			return e
		}
		if err = updateCommand(ctx, a, "sudo", "setcap", "cap_net_bind_service=+ep", nginx); err != nil {
			return err
		}
	}
	if vector {
		if err = updateCommand(ctx, a, "yay", "-S", "--needed", "pgvector"); err != nil {
			return err
		}
	}
	if containsPackage(string(plan), "dnsmasq") && exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", "NetworkManager").Run() == nil {
		if err = updateCommand(ctx, a, "sudo", "systemctl", "reload", "NetworkManager"); err != nil {
			return err
		}
	}
	return nil
}
func containsPackage(plan, name string) bool {
	for _, line := range strings.Split(plan, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == name {
			return true
		}
	}
	return false
}
