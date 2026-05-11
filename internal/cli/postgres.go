package cli

import (
	"context"
	"fmt"

	"herdlite/internal/app"
	"herdlite/internal/postgres"
)

func runPostgres(a *app.App, args []string) int {
	if len(args) == 0 || hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite postgres <init|start|stop|status|logs>")
		return 0
	}

	manager := postgres.Manager{Paths: a.Paths, Out: a.Out}
	switch args[0] {
	case "init":
		if err := a.InitUserDirs(); err != nil {
			fmt.Fprintf(a.Err, "postgres init: %v\n", err)
			return 1
		}
		if err := manager.Init(context.Background()); err != nil {
			fmt.Fprintf(a.Err, "postgres init: %v\n", err)
			return 1
		}
	case "start":
		if err := a.InitUserDirs(); err != nil {
			fmt.Fprintf(a.Err, "postgres start: %v\n", err)
			return 1
		}
		if err := manager.Start(context.Background()); err != nil {
			fmt.Fprintf(a.Err, "postgres start: %v\n", err)
			return 1
		}
	case "stop":
		if err := manager.Stop(context.Background()); err != nil {
			fmt.Fprintf(a.Err, "postgres stop: %v\n", err)
			return 1
		}
	case "status":
		if err := manager.Status(context.Background()); err != nil {
			fmt.Fprintf(a.Err, "postgres status: %v\n", err)
			return 1
		}
	case "logs":
		fmt.Fprintln(a.Out, manager.LogPath())
	default:
		fmt.Fprintf(a.Err, "unknown postgres command: %s\n", args[0])
		return 1
	}
	return 0
}
