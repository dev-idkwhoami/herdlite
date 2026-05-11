package cli

import (
	"fmt"

	"herdlite/internal/app"
)

func runNode(a *app.App, args []string) int {
	if len(args) == 0 || hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite node global <version>")
		return 0
	}

	switch args[0] {
	case "global":
		return runNodeGlobal(a, args[1:])
	default:
		fmt.Fprintf(a.Err, "unknown node command: %s\n", args[0])
		return 1
	}
}

func runNodeGlobal(a *app.App, args []string) int {
	if hasHelp(args) || len(args) != 1 {
		fmt.Fprintln(a.Out, "Usage: herdlite node global <version>")
		return codeForUsage(args, 1)
	}
	if err := a.InitUserDirs(); err != nil {
		fmt.Fprintf(a.Err, "node global: %v\n", err)
		return 1
	}
	if err := a.Store.SetMetaValue("global_node_version", args[0]); err != nil {
		fmt.Fprintf(a.Err, "node global: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.Out, "Global Node set to %s\n", args[0])
	return 0
}
