package cli

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"herdlite/internal/app"
	"herdlite/internal/phpmanager"
)

func runPHPExtensions(a *app.App, args []string) int {
	const usage = "Usage:\n  herdlite php extensions open\n  herdlite php extensions list\n  herdlite php extensions sync <version> [--dry-run]"
	if len(args) == 0 || hasHelp(args) {
		fmt.Fprintln(a.Out, usage)
		return 0
	}
	if len(args) == 1 {
		switch args[0] {
		case "open":
			return runPHPExtensionsOpen(a)
		case "list":
			return runPHPExtensionsList(a)
		}
	}
	if len(args) < 2 || args[0] != "sync" || len(args) > 3 || (len(args) == 3 && args[2] != "--dry-run") {
		fmt.Fprintln(a.Err, usage)
		return 1
	}
	dryRun := len(args) == 3
	runtime, found, err := a.Store.PHPRuntimeForRequest(args[1])
	if err != nil {
		fmt.Fprintf(a.Err, "php extensions: %v\n", err)
		return 1
	}
	if !found {
		fmt.Fprintf(a.Err, "php extensions: PHP %s is not installed\n", args[1])
		return 1
	}
	i := phpmanager.Installer{Paths: a.Paths, Store: a.Store, Out: a.Out, Err: a.Err}
	if err := i.SyncExtensions(context.Background(), runtime, dryRun); err != nil {
		fmt.Fprintf(a.Err, "php extensions: %v\n", err)
		return 1
	}
	if dryRun {
		fmt.Fprintln(a.Out, "Dry run only; no extensions were installed.")
	} else {
		fmt.Fprintln(a.Out, "Extensions synced. Restart running PHP-FPM and queue workers to load changes.")
	}
	return 0
}

func runPHPExtensionsOpen(a *app.App) int {
	opener, err := exec.LookPath("xdg-open")
	if err != nil {
		fmt.Fprintf(a.Err, "php extensions open: xdg-open not found; open %s manually\n", (phpmanager.Installer{Paths: a.Paths, Store: a.Store}).ExtensionConfigPath())
		return 1
	}
	path, err := (phpmanager.Installer{Paths: a.Paths, Store: a.Store}).EnsureExtensionConfig()
	if err != nil {
		fmt.Fprintf(a.Err, "php extensions open: %v\n", err)
		return 1
	}
	cmd := exec.Command(opener, path)
	cmd.Stdout, cmd.Stderr = a.Out, a.Err
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(a.Err, "php extensions open: %v; open %s manually\n", err, path)
		return 1
	}
	fmt.Fprintf(a.Out, "Opened %s\n", path)
	return 0
}

func runPHPExtensionsList(a *app.App) int {
	runtimes, err := a.Store.PHPRuntimes()
	if err != nil {
		fmt.Fprintf(a.Err, "php extensions list: %v\n", err)
		return 1
	}
	if len(runtimes) == 0 {
		fmt.Fprintln(a.Out, "No PHP runtimes installed yet.")
		return 0
	}
	code := 0
	i := phpmanager.Installer{Paths: a.Paths, Err: a.Err}
	for _, php := range runtimes {
		modules, err := i.LoadedExtensions(context.Background(), php)
		if err != nil {
			fmt.Fprintf(a.Err, "PHP %s: %v\n", php.Version, err)
			code = 1
			continue
		}
		fmt.Fprintf(a.Out, "PHP %s — %d loaded extensions (CLI)\n", php.Version, len(modules))
		if len(modules) == 0 {
			fmt.Fprintln(a.Out, "  (none)")
		} else {
			fmt.Fprintf(a.Out, "  %s\n", strings.Join(modules, ", "))
		}
	}
	return code
}
