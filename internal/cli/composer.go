package cli

import (
	"context"
	"fmt"

	"herdlite/internal/app"
	"herdlite/internal/composer"
)

func runComposer(a *app.App, args []string) int {
	if len(args) == 0 || hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite composer <install|path>")
		return 0
	}

	switch args[0] {
	case "install":
		return runComposerInstall(a, args[1:])
	case "path":
		if len(args) != 1 {
			fmt.Fprintln(a.Err, "Usage: herdlite composer path")
			return 1
		}
		fmt.Fprintln(a.Out, (composer.Manager{Paths: a.Paths}).Path())
		return 0
	default:
		fmt.Fprintf(a.Err, "unknown composer command: %s\n", args[0])
		return 1
	}
}

func runComposerInstall(a *app.App, args []string) int {
	dryRun := false
	for _, arg := range args {
		switch arg {
		case "--dry-run":
			dryRun = true
		default:
			fmt.Fprintf(a.Err, "unknown composer install option: %s\n", arg)
			return 1
		}
	}

	if !dryRun {
		if err := a.InitUserDirs(); err != nil {
			fmt.Fprintf(a.Err, "composer install: %v\n", err)
			return 1
		}
	}
	runtime, found, err := a.Store.LatestPHPRuntime()
	if err != nil {
		fmt.Fprintf(a.Err, "composer install: %v\n", err)
		return 1
	}
	if !found {
		fmt.Fprintln(a.Err, "composer install: no PHP runtime installed; run `herdlite php install latest` first")
		return 1
	}
	if err := (composer.Manager{Paths: a.Paths, Out: a.Out}).Install(context.Background(), runtime, dryRun); err != nil {
		fmt.Fprintf(a.Err, "composer install: %v\n", err)
		return 1
	}
	if !dryRun {
		fmt.Fprintf(a.Out, "Installed Composer: %s\n", (composer.Manager{Paths: a.Paths}).Path())
	}
	return 0
}
