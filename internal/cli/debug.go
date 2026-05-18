package cli

import (
	"fmt"
	"os/exec"

	"herdlite/internal/app"
	"herdlite/internal/daemon"
	"herdlite/internal/debugui"
)

func runDebug(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite debug")
		return 0
	}
	if len(args) != 0 {
		fmt.Fprintln(a.Err, "Usage: herdlite debug")
		return 1
	}
	if !(daemon.Manager{Paths: a.Paths, Store: a.Store, Out: a.Out}).Status().Healthy {
		fmt.Fprintln(a.Err, "debug: daemon is not running; run `herdlite start` first")
		return 1
	}
	if err := openDebugApp(); err != nil {
		fmt.Fprintf(a.Err, "debug: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.Out, "Opened %s\n", debugui.BaseAppURL)
	return 0
}

func openDebugApp() error {
	opener, err := exec.LookPath("xdg-open")
	if err != nil {
		return fmt.Errorf("xdg-open not found")
	}
	if err := exec.Command(opener, debugui.BaseAppURL).Start(); err != nil {
		return err
	}
	return nil
}
