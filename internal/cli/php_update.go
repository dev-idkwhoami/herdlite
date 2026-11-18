package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"herdlite/internal/app"
	"herdlite/internal/phpmanager"
	"herdlite/internal/services"
	"herdlite/internal/state"
)

func runPHPLock(a *app.App, args []string, locked bool) int {
	command := "unlock"
	if locked {
		command = "lock"
	}
	if hasHelp(args) || len(args) != 1 {
		fmt.Fprintf(a.Out, "Usage: herdlite php %s <exact-version>\n", command)
		return codeForUsage(args, 1)
	}
	if err := a.Store.SetPHPLocked(args[0], locked); err != nil {
		fmt.Fprintf(a.Err, "php %s: %v\n", command, err)
		return 1
	}
	fmt.Fprintf(a.Out, "PHP %s %sed.\n", args[0], command)
	return 0
}

type phpUpdateOptions struct {
	minor                             string
	dryRun, force, keepBuild, verbose bool
}

func parsePHPUpdateOptions(args []string) (phpUpdateOptions, error) {
	var o phpUpdateOptions
	for _, arg := range args {
		switch arg {
		case "--dry-run":
			o.dryRun = true
		case "--force":
			o.force = true
		case "--keep-build":
			o.keepBuild = true
		case "--verbose":
			o.verbose = true
		default:
			if strings.HasPrefix(arg, "-") {
				return o, fmt.Errorf("unknown option: %s", arg)
			}
			if o.minor != "" {
				return o, fmt.Errorf("php update accepts only one minor version")
			}
			o.minor = arg
		}
	}
	return o, nil
}

func printPHPUpdatePlan(out io.Writer, plans []phpmanager.UpdatePlan) {
	for _, p := range plans {
		for _, r := range p.Locked {
			fmt.Fprintf(out, "PHP %s: locked; keep runtime and project pins\n", r.Version)
		}
		if len(p.Old) == 0 {
			continue
		}
		if !p.Needed() {
			fmt.Fprintf(out, "PHP %s: already current\n", p.Release.Version)
			continue
		}
		for _, old := range p.Old {
			if old.Version == p.Release.Version && !p.Force {
				fmt.Fprintf(out, "PHP %s: already current; keep\n", old.Version)
			} else {
				fmt.Fprintf(out, "PHP %s -> %s\n", old.Version, p.Release.Version)
			}
		}
		if p.Force {
			fmt.Fprintln(out, "  Rebuild target even if already installed.")
		}
		for _, project := range p.Projects {
			fmt.Fprintf(out, "  Project %s: %s -> %s\n", project.Name, project.PHPVersion, p.Release.Version)
		}
		if p.Global {
			fmt.Fprintf(out, "  Global PHP -> %s\n", p.Release.Version)
		}
		if p.Problem != "" {
			fmt.Fprintf(out, "  BLOCKED: %s\n", p.Problem)
		}
	}
}

// One reader for the entire session avoids losing buffered answers between prompts.
func updatePrompt(ctx context.Context, out io.Writer, in io.Reader) func(string) bool {
	scanner := bufio.NewScanner(in)
	return func(question string) bool {
		if ctx.Err() != nil {
			return false
		}
		fmt.Fprintf(out, "%s [y/N] ", question)
		answers := make(chan bool, 1)
		go func() {
			if !scanner.Scan() {
				answers <- false
				return
			}
			answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
			answers <- answer == "y" || answer == "yes"
		}()
		select {
		case answer := <-answers:
			return answer
		case <-ctx.Done():
			return false
		}
	}
}

func runPHPUpdate(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite php update [minor] [--dry-run] [--force] [--keep-build] [--verbose]")
		return 0
	}
	opts, err := parsePHPUpdateOptions(args)
	if err != nil {
		fmt.Fprintln(a.Err, err)
		return 1
	}
	// Avoid creating state or user directories for a dry run on an empty installation.
	if _, err := os.Stat(a.Paths.StateFile); os.IsNotExist(err) {
		fmt.Fprintln(a.Out, "No PHP runtimes installed.")
		if opts.minor != "" {
			return 1
		}
		return 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	installer := phpmanager.Installer{Paths: a.Paths, Store: a.Store, Out: a.Out, Err: a.Err}
	runtimes, err := a.Store.PHPRuntimes()
	if err != nil {
		fmt.Fprintln(a.Err, err)
		return 1
	}
	// Resolve nothing when every selected runtime is locked (also works offline).
	unlocked := false
	for _, r := range runtimes {
		if opts.minor != "" && r.Minor != opts.minor {
			continue
		}
		locked, e := a.Store.PHPLocked(r.Version)
		if e != nil {
			fmt.Fprintln(a.Err, e)
			return 1
		}
		unlocked = unlocked || !locked
	}
	var releases []phpmanager.Release
	if unlocked {
		releases, err = (phpmanager.ReleaseClient{}).Releases(ctx)
		if err != nil {
			fmt.Fprintf(a.Err, "php update: %v\n", err)
			return 1
		}
	}
	plans, err := installer.PlanUpdates(releases, opts.minor, opts.force)
	if err != nil {
		fmt.Fprintf(a.Err, "php update: %v\n", err)
		return 1
	}
	printPHPUpdatePlan(a.Out, plans)
	needed := false
	actionable := false
	blocked := false
	for _, p := range plans {
		needed = needed || p.Needed()
		actionable = actionable || (p.Needed() && p.Problem == "")
		blocked = blocked || p.Problem != ""
	}
	if opts.dryRun {
		fmt.Fprintln(a.Out, "Dry run: no runtimes or services changed.")
		if blocked {
			return 1
		}
		return 0
	}
	if !needed {
		fmt.Fprintln(a.Out, "Nothing to update.")
		return 0
	}
	if blocked && !actionable {
		fmt.Fprintln(a.Err, "Resolve the configuration conflicts above or lock those patches before updating.")
		return 1
	}
	info, err := os.Stdin.Stat()
	interactive := err == nil && info.Mode()&os.ModeCharDevice != 0
	if !interactive {
		fmt.Fprintln(a.Err, "php update requires an interactive terminal for confirmation; use --dry-run to preview")
		return 1
	}
	prompt := updatePrompt(ctx, a.Out, os.Stdin)
	if !prompt("Stop hosting and PHP workers, apply this plan, then restore their previous running state?") {
		fmt.Fprintln(a.Out, "Update cancelled.")
		if ctx.Err() != nil {
			return 1
		}
		return 0
	}
	if err := a.InitUserDirs(); err != nil {
		fmt.Fprintln(a.Err, err)
		return 1
	}
	manager := services.HostingManager{Paths: a.Paths, Store: a.Store, Out: a.Out}
	return executePHPUpdates(ctx, a, plans, manager, func(ctx context.Context, p phpmanager.UpdatePlan) (*phpmanager.UpdateResult, error) {
		return installer.ApplyUpdate(ctx, p, opts.verbose, opts.keepBuild)
	}, prompt)
}

type phpMaintenance interface {
	CapturePHPState() (services.MaintenanceState, error)
	PausePHP(context.Context, services.MaintenanceState) error
	RestorePHP(context.Context, services.MaintenanceState, map[string]state.PHPRuntime) error
}

func executePHPUpdates(ctx context.Context, a *app.App, plans []phpmanager.UpdatePlan, manager phpMaintenance, apply func(context.Context, phpmanager.UpdatePlan) (*phpmanager.UpdateResult, error), prompt func(string) bool) (code int) {
	previous, err := manager.CapturePHPState()
	if err != nil {
		fmt.Fprintf(a.Err, "capture service state: %v\n", err)
		return 1
	}
	replacements := map[string]state.PHPRuntime{}
	var completed []*phpmanager.UpdateResult
	// This runs on build errors, declined continuation and signal cancellation too.
	defer func() {
		fmt.Fprintln(a.Out, "Restoring previous service state...")
		if err := manager.RestorePHP(context.Background(), previous, replacements); err != nil {
			code = 1
			fmt.Fprintf(a.Err, "Service restoration failed: %v\n", err)
			if len(completed) == 0 {
				return
			}
			active, e := manager.CapturePHPState()
			if e == nil {
				e = manager.PausePHP(context.Background(), active)
			}
			if e != nil {
				fmt.Fprintf(a.Err, "Cannot safely roll back running services: %v. Old runtime directories retained.\n", e)
				return
			}
			recovered := true
			for n := len(completed) - 1; n >= 0; n-- {
				if e := completed[n].Rollback(); e != nil {
					recovered = false
					fmt.Fprintf(a.Err, "ROLLBACK FAILED: %v\n", e)
				}
			}
			if recovered {
				fmt.Fprintln(a.Err, "Updates rolled back because services could not resume.")
			}
			if e := manager.RestorePHP(context.Background(), previous, nil); e != nil {
				fmt.Fprintf(a.Err, "Previous services could not be restored: %v\n", e)
			}
			return
		}
		for _, r := range completed {
			if err := r.Cleanup(); err != nil {
				code = 1
				fmt.Fprintf(a.Err, "PHP %s updated, but old runtime cleanup failed: %v\n", r.Runtime.Version, err)
			}
		}
	}()
	if err := manager.PausePHP(ctx, previous); err != nil {
		fmt.Fprintf(a.Err, "Stopping services failed: %v\n", err)
		return 1
	}
	for n, p := range plans {
		if !p.Needed() {
			continue
		}
		if ctx.Err() != nil {
			fmt.Fprintln(a.Err, "Update interrupted.")
			return 1
		}
		result, err := apply(ctx, p)
		if err != nil {
			code = 1
			fmt.Fprintf(a.Err, "PHP %s update failed: %v\n", p.Minor, err)
			if result != nil {
				fmt.Fprintln(a.Err, "Recovery is incomplete; stopping the batch.")
				return 1
			}
			fmt.Fprintln(a.Err, "Previous installation preserved.")
			remaining := false
			for _, next := range plans[n+1:] {
				remaining = remaining || next.Needed()
			}
			if ctx.Err() != nil || !remaining || !prompt("Continue with the remaining PHP versions?") {
				return 1
			}
			continue
		}
		completed = append(completed, result)
		for _, old := range p.Old {
			replacements[old.Version] = result.Runtime
		}
		fmt.Fprintf(a.Out, "Updated PHP %s to %s.\n", p.Minor, result.Runtime.Version)
	}
	return code
}
