package cli

import (
	"fmt"
	"os/exec"

	"herdlite/internal/app"
	"herdlite/internal/daemon"
	"herdlite/internal/debugui"
)

func runLogs(a *app.App, args []string) int {
	if len(args) == 0 || hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite logs open")
		return codeForUsage(args, 1)
	}
	switch args[0] {
	case "open":
		if len(args) != 1 {
			fmt.Fprintln(a.Err, "Usage: herdlite logs open")
			return 1
		}
		if !(daemon.Manager{Paths: a.Paths, Store: a.Store, Out: a.Out}).Status().Healthy {
			fmt.Fprintln(a.Err, "logs open: daemon is not running; run `herdlite start` first")
			return 1
		}
		opener, err := exec.LookPath("xdg-open")
		if err != nil {
			fmt.Fprintln(a.Err, "logs open: xdg-open not found")
			return 1
		}
		url := debugui.BaseURL + "/app/logs"
		if err := exec.Command(opener, url).Start(); err != nil {
			fmt.Fprintf(a.Err, "logs open: %v\n", err)
			return 1
		}
		fmt.Fprintf(a.Out, "Opened %s\n", url)
		return 0
	default:
		fmt.Fprintf(a.Err, "unknown logs command: %s\n", args[0])
		return 1
	}
}
