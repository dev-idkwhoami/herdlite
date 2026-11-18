package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"herdlite/internal/app"
	"herdlite/internal/phpmanager"
	"herdlite/internal/services"
	"herdlite/internal/state"
)

// Read all prompts through one scanner, with cancellation while awaiting input.
func removalReader(ctx context.Context, in io.Reader) func() (string, error) {
	scanner := bufio.NewScanner(in)
	return func() (string, error) {
		type answer struct {
			text string
			err  error
		}
		ch := make(chan answer, 1)
		go func() {
			if scanner.Scan() {
				ch <- answer{text: strings.TrimSpace(scanner.Text())}
			} else {
				err := scanner.Err()
				if err == nil {
					err = io.EOF
				}
				ch <- answer{err: err}
			}
		}()
		select {
		case a := <-ch:
			return a.text, a.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

func chooseRemovalTarget(out io.Writer, read func() (string, error), label string, choices []state.PHPRuntime) (string, error) {
	fmt.Fprintf(out, "Choose replacement PHP for %s:\n", label)
	for n, r := range choices {
		fmt.Fprintf(out, "  %d) PHP %s\n", n+1, r.Version)
	}
	for {
		fmt.Fprint(out, "Version or number (q to cancel): ")
		answer, err := read()
		if err != nil {
			return "", err
		}
		if strings.EqualFold(answer, "q") {
			return "", fmt.Errorf("removal cancelled")
		}
		for _, r := range choices {
			if answer == r.Version {
				return r.Version, nil
			}
		}
		if n, err := strconv.Atoi(answer); err == nil && n > 0 && n <= len(choices) {
			return choices[n-1].Version, nil
		}
		fmt.Fprintln(out, "Choose one of the listed installed versions.")
	}
}

// Select one target for every reference to the removed patch.
func selectRemovalReplacement(out io.Writer, read func() (string, error), plan *phpmanager.RemovalPlan) error {
	version := plan.Before.Old[0].Version
	replaceGlobal := plan.Before.HasGlobal && plan.Before.Global == version
	if len(plan.Before.Projects) == 0 && !replaceGlobal {
		return nil
	}
	fmt.Fprintf(out, "PHP %s is used by %d project(s).\n", version, len(plan.Before.Projects))
	for _, project := range plan.Before.Projects {
		fmt.Fprintf(out, "  %s\n", project.Name)
	}
	if replaceGlobal {
		fmt.Fprintln(out, "  Global PHP selection")
	}
	target, err := chooseRemovalTarget(out, read, "all references to PHP "+version, plan.Choices)
	if err != nil {
		return err
	}
	plan.Replacements = make(map[string]string, len(plan.Before.Projects))
	for _, project := range plan.Before.Projects {
		plan.Replacements[project.Name] = target
	}
	if replaceGlobal {
		plan.GlobalReplacement = target
	}
	return nil
}

func runPHPRemove(a *app.App, args []string) int {
	if hasHelp(args) || len(args) != 1 {
		fmt.Fprintln(a.Out, "Usage: herdlite php remove <exact-version>")
		return codeForUsage(args, 1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	i := phpmanager.Installer{Paths: a.Paths, Store: a.Store, Out: a.Out, Err: a.Err}
	plan, err := i.PlanRemoval(args[0])
	if err != nil {
		fmt.Fprintf(a.Err, "php remove: %v\n", err)
		return 1
	}
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		fmt.Fprintln(a.Err, "php remove requires an interactive terminal to select replacements and confirm removal")
		return 1
	}
	read := removalReader(ctx, os.Stdin)
	if err = selectRemovalReplacement(a.Out, read, &plan); err != nil {
		fmt.Fprintln(a.Err, err)
		return 1
	}
	if err = i.ValidateRemoval(ctx, plan); err != nil {
		fmt.Fprintf(a.Err, "php remove: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.Out, "Remove PHP %s (%s)\n", args[0], plan.Before.Old[0].Prefix)
	for _, p := range plan.Before.Projects {
		fmt.Fprintf(a.Out, "  %s: %s -> %s\n", p.Name, p.PHPVersion, plan.Replacements[p.Name])
	}
	if plan.GlobalReplacement != "" {
		fmt.Fprintf(a.Out, "  Global PHP: %s -> %s\n", args[0], plan.GlobalReplacement)
	}
	fmt.Fprint(a.Out, "Stop hosting and PHP workers, migrate these references, and remove this runtime? [y/N] ")
	answer, err := read()
	if err != nil || !(strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes")) {
		fmt.Fprintln(a.Out, "Removal cancelled.")
		if ctx.Err() != nil {
			return 1
		}
		return 0
	}
	manager := services.HostingManager{Paths: a.Paths, Store: a.Store, Out: a.Out}
	return executePHPRemoval(ctx, a, plan, manager, i.ApplyRemoval)
}

// Restore the replacement FPM only when the removed FPM was active.
func removalServiceState(before services.MaintenanceState, p phpmanager.RemovalPlan) services.MaintenanceState {
	after := before
	after.FPM = nil
	removedActive := false
	for _, r := range before.FPM {
		if r.Version == p.Before.Old[0].Version {
			removedActive = true
		} else {
			after.FPM = append(after.FPM, r)
		}
	}
	if removedActive {
		for _, project := range p.Before.Projects {
			if project.Enabled {
				if target, err := p.Target(p.Replacements[project.Name]); err == nil {
					after.FPM = append(after.FPM, target)
					return after
				}
			}
		}
	}
	return after
}

func executePHPRemoval(ctx context.Context, a *app.App, p phpmanager.RemovalPlan, m phpMaintenance, apply func(context.Context, phpmanager.RemovalPlan) (*phpmanager.UpdateResult, error)) (code int) {
	before, err := m.CapturePHPState()
	if err != nil {
		fmt.Fprintln(a.Err, err)
		return 1
	}
	after := before
	var result *phpmanager.UpdateResult
	defer func() {
		if err := m.RestorePHP(context.Background(), after, nil); err != nil {
			code = 1
			fmt.Fprintf(a.Err, "Service restoration failed: %v\n", err)
			if result == nil {
				return
			}
			active, e := m.CapturePHPState()
			if e == nil {
				e = m.PausePHP(context.Background(), active)
			}
			if e != nil {
				fmt.Fprintf(a.Err, "Cannot safely roll back running services: %v; old runtime directory retained.\n", e)
				return
			}
			if e = result.Rollback(); e != nil {
				fmt.Fprintf(a.Err, "ROLLBACK FAILED: %v\n", e)
				return
			}
			fmt.Fprintln(a.Err, "Removal rolled back.")
			if e = m.RestorePHP(context.Background(), before, nil); e != nil {
				fmt.Fprintf(a.Err, "Previous service state could not be restored: %v\n", e)
			}
			return
		}
		if result != nil && code == 0 {
			if err := result.Cleanup(); err != nil {
				code = 1
				fmt.Fprintf(a.Err, "PHP unregistered, but directory cleanup failed: %v\n", err)
				return
			}
			fmt.Fprintf(a.Out, "Removed PHP %s. Extension manifest settings retained for future reinstalls.\n", p.Before.Old[0].Version)
		}
	}()
	if err := m.PausePHP(ctx, before); err != nil {
		fmt.Fprintf(a.Err, "Stopping services failed: %v\n", err)
		return 1
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintln(a.Err, err)
		return 1
	}
	result, err = apply(ctx, p)
	if err != nil {
		fmt.Fprintf(a.Err, "Removal failed: %v\n", err)
		return 1
	}
	after = removalServiceState(before, p)
	return 0
}
