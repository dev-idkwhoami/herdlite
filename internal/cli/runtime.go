package cli

import (
	"context"
	"fmt"
	"os/exec"

	"herdlite/internal/app"
	"herdlite/internal/services"
)

func runStart(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite start")
		return 0
	}

	if err := a.InitUserDirs(); err != nil {
		fmt.Fprintf(a.Err, "start: %v\n", err)
		return 1
	}

	manager := services.HostingManager{
		Paths: a.Paths,
		Store: a.Store,
		Out:   a.Out,
	}
	if err := manager.Start(context.Background()); err != nil {
		fmt.Fprintf(a.Err, "start: %v\n", err)
		return 1
	}

	fmt.Fprintln(a.Out, "Hosting services are running.")
	return 0
}

func runStop(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite stop")
		return 0
	}

	manager := services.HostingManager{
		Paths: a.Paths,
		Store: a.Store,
		Out:   a.Out,
	}
	if err := manager.Stop(context.Background()); err != nil {
		fmt.Fprintf(a.Err, "stop: %v\n", err)
		return 1
	}

	fmt.Fprintln(a.Out, "Hosting services are stopped.")
	return 0
}

func runRestart(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite restart")
		return 0
	}

	manager := services.HostingManager{
		Paths: a.Paths,
		Store: a.Store,
		Out:   a.Out,
	}
	if err := manager.Stop(context.Background()); err != nil {
		fmt.Fprintf(a.Err, "restart: %v\n", err)
		return 1
	}
	if err := manager.Start(context.Background()); err != nil {
		fmt.Fprintf(a.Err, "restart: %v\n", err)
		return 1
	}

	fmt.Fprintln(a.Out, "Hosting services are restarted.")
	return 0
}

func runPaths(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite paths")
		return 0
	}

	printPaths(a)
	return 0
}

func runOpen(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite open [--p <path|name|domain>]")
		return 0
	}

	selector, args, err := splitProjectSelector(args)
	if err != nil {
		fmt.Fprintf(a.Err, "open: %v\n", err)
		return 1
	}
	if len(args) != 0 {
		fmt.Fprintf(a.Err, "unknown open option: %s\n", args[0])
		return 1
	}

	project, err := resolveExistingProject(a, selector)
	if err != nil {
		fmt.Fprintf(a.Err, "open: %v\n", err)
		return 1
	}

	url := "https://" + project.Domain
	opener, err := exec.LookPath("xdg-open")
	if err != nil {
		fmt.Fprintf(a.Err, "open: xdg-open not found; open %s manually\n", url)
		return 1
	}
	cmd := exec.Command(opener, url)
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(a.Err, "open: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.Out, "Opened %s\n", url)
	return 0
}

func printPaths(a *app.App) {
	fmt.Fprintf(a.Out, "  config: %s\n", a.Paths.ConfigDir)
	fmt.Fprintf(a.Out, "  data:   %s\n", a.Paths.DataDir)
	fmt.Fprintf(a.Out, "  cache:  %s\n", a.Paths.CacheDir)
	fmt.Fprintf(a.Out, "  state:  %s\n", a.Paths.StateFile)
	fmt.Fprintf(a.Out, "  logs:   %s\n", a.Paths.LogDir)
}
