package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"herdlite/internal/app"
	"herdlite/internal/daemon"
)

func runDaemon(a *app.App, args []string) int {
	if len(args) == 0 || hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite daemon <start|stop|status|run>")
		return 0
	}

	switch args[0] {
	case "start":
		return runDaemonStart(a, args[1:])
	case "stop":
		return runDaemonStop(a, args[1:])
	case "status":
		return runDaemonStatus(a, args[1:])
	case "run":
		return runDaemonRun(a, args[1:])
	default:
		fmt.Fprintf(a.Err, "unknown daemon command: %s\n", args[0])
		return 1
	}
}

func runDaemonStart(a *app.App, args []string) int {
	if hasHelp(args) || len(args) != 0 {
		fmt.Fprintln(a.Out, "Usage: herdlite daemon start")
		return codeForUsage(args, 0)
	}
	if err := a.InitUserDirs(); err != nil {
		fmt.Fprintf(a.Err, "daemon start: %v\n", err)
		return 1
	}
	if err := (daemon.Manager{Paths: a.Paths, Store: a.Store, Out: a.Out}).Start(context.Background()); err != nil {
		fmt.Fprintf(a.Err, "daemon start: %v\n", err)
		return 1
	}
	return 0
}

func runDaemonStop(a *app.App, args []string) int {
	if hasHelp(args) || len(args) != 0 {
		fmt.Fprintln(a.Out, "Usage: herdlite daemon stop")
		return codeForUsage(args, 0)
	}
	if err := (daemon.Manager{Paths: a.Paths, Store: a.Store, Out: a.Out}).Stop(); err != nil {
		fmt.Fprintf(a.Err, "daemon stop: %v\n", err)
		return 1
	}
	return 0
}

func runDaemonStatus(a *app.App, args []string) int {
	if hasHelp(args) || len(args) != 0 {
		fmt.Fprintln(a.Out, "Usage: herdlite daemon status")
		return codeForUsage(args, 0)
	}
	status := (daemon.Manager{Paths: a.Paths, Store: a.Store, Out: a.Out}).Status()
	if !status.Running {
		fmt.Fprintln(a.Out, "Daemon is stopped.")
		return 0
	}
	fmt.Fprintln(a.Out, "Daemon is running.")
	fmt.Fprintf(a.Out, "  pid:     %d\n", status.PID)
	fmt.Fprintf(a.Out, "  healthy: %v\n", status.Healthy)
	return 0
}

func runDaemonRun(a *app.App, args []string) int {
	if hasHelp(args) || len(args) != 0 {
		fmt.Fprintln(a.Out, "Usage: herdlite daemon run")
		return codeForUsage(args, 0)
	}
	if err := a.InitUserDirs(); err != nil {
		fmt.Fprintf(a.Err, "daemon run: %v\n", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := (daemon.Service{Paths: a.Paths, Store: a.Store, Out: a.Err}).Run(ctx); err != nil {
		fmt.Fprintf(a.Err, "daemon run: %v\n", err)
		return 1
	}
	return 0
}
