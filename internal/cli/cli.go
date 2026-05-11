package cli

import (
	"fmt"
	"os"

	"herdlite/internal/app"
	"herdlite/internal/buildinfo"
)

type command struct {
	Name        string
	Usage       string
	Description string
	Advanced    bool
	Run         func(*app.App, []string) int
}

func Run(args []string) int {
	a, err := app.New(os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "herdlite: %v\n", err)
		return 1
	}

	commands := allCommands()
	if len(args) == 0 {
		printHelp(a, commands, false)
		return 0
	}

	name := args[0]
	if name == "help" || name == "-h" || name == "--help" {
		printHelp(a, commands, false)
		return 0
	}

	if name == "--helpd" || name == "helpd" {
		printHelp(a, commands, true)
		return 0
	}

	if name == "version" || name == "--version" {
		fmt.Fprintf(a.Out, "herdlite %s (%s, %s)\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date)
		return 0
	}

	for _, cmd := range commands {
		if cmd.Name == name {
			return cmd.Run(a, args[1:])
		}
	}

	fmt.Fprintf(a.Err, "unknown command: %s\n\n", name)
	printHelp(a, commands, false)
	return 1
}

func allCommands() []command {
	return []command{
		{
			Name:        "install",
			Usage:       "herdlite install [--dry-run]",
			Description: "prepare system integration points",
			Run:         runInstall,
		},
		{
			Name:        "doctor",
			Usage:       "herdlite doctor",
			Description: "print environment diagnostics",
			Advanced:    true,
			Run:         runDoctor,
		},
		{
			Name:        "repair",
			Usage:       "herdlite repair [--dry-run]",
			Description: "repair global system integration",
			Advanced:    true,
			Run:         runRepair,
		},
		{
			Name:        "uninstall",
			Usage:       "herdlite uninstall [--dry-run] [--purge]",
			Description: "remove global system integration",
			Advanced:    true,
			Run:         runUninstall,
		},
		{
			Name:        "link",
			Usage:       "herdlite link [--p <path|name|domain>] [--php <version>] [--ws]",
			Description: "register a Laravel project",
			Run:         runLink,
		},
		{
			Name:        "list",
			Usage:       "herdlite list",
			Description: "list registered projects",
			Run:         runList,
		},
		{
			Name:        "start",
			Usage:       "herdlite start",
			Description: "start local hosting services",
			Run:         runStart,
		},
		{
			Name:        "stop",
			Usage:       "herdlite stop",
			Description: "stop local hosting services",
			Advanced:    true,
			Run:         runStop,
		},
		{
			Name:        "restart",
			Usage:       "herdlite restart",
			Description: "restart local hosting services",
			Advanced:    true,
			Run:         runRestart,
		},
		{
			Name:        "open",
			Usage:       "herdlite open [--p <path|name|domain>]",
			Description: "open a linked project in the browser",
			Run:         runOpen,
		},
		{
			Name:        "mail",
			Usage:       "herdlite mail [list|show|open|raw|clear]",
			Description: "view captured local mail",
			Run:         runMail,
		},
		{
			Name:        "logs",
			Usage:       "herdlite logs open",
			Description: "open daemon log viewer",
			Run:         runLogs,
		},
		{
			Name:        "dumps",
			Usage:       "herdlite dumps open",
			Description: "open captured dump viewer",
			Run:         runDumps,
		},
		{
			Name:        "env",
			Usage:       "herdlite env apply [--p <path|name|domain>]",
			Description: "apply Laravel environment settings",
			Advanced:    true,
			Run:         runEnv,
		},
		{
			Name:        "paths",
			Usage:       "herdlite paths",
			Description: "print Herdlite filesystem paths",
			Advanced:    true,
			Run:         runPaths,
		},
		{
			Name:        "php",
			Usage:       "herdlite php <available|install|list>",
			Description: "manage Herdlite-built PHP runtimes",
			Advanced:    true,
			Run:         runPHP,
		},
		{
			Name:        "composer",
			Usage:       "herdlite composer <install|path>",
			Description: "manage Herdlite Composer",
			Advanced:    true,
			Run:         runComposer,
		},
		{
			Name:        "node",
			Usage:       "herdlite node global <version>",
			Description: "manage global Node default",
			Advanced:    true,
			Run:         runNode,
		},
		{
			Name:        "postgres",
			Usage:       "herdlite postgres <init|start|stop|status>",
			Description: "manage Herdlite PostgreSQL",
			Advanced:    true,
			Run:         runPostgres,
		},
		{
			Name:        "cert",
			Usage:       "herdlite cert <init|site|trust>",
			Description: "manage local development certificates",
			Advanced:    true,
			Run:         runCert,
		},
		{
			Name:        "shim",
			Usage:       "herdlite shim <php|composer|node|npm|npx>",
			Description: "run project-aware shell shims",
			Advanced:    true,
			Run:         runShim,
		},
		{
			Name:        "daemon",
			Usage:       "herdlite daemon <start|stop|status|run>",
			Description: "manage Herdlite daemon",
			Advanced:    true,
			Run:         runDaemon,
		},
	}
}

func printHelp(a *app.App, commands []command, full bool) {
	fmt.Fprintln(a.Out, "Herdlite - personal Laravel development runtime")
	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Usage:")
	fmt.Fprintln(a.Out, "  herdlite <command> [args]")
	fmt.Fprintln(a.Out)
	if full {
		fmt.Fprintln(a.Out, "Commands:")
	} else {
		fmt.Fprintln(a.Out, "Common commands:")
	}
	for _, cmd := range commands {
		if cmd.Advanced && !full {
			continue
		}
		fmt.Fprintf(a.Out, "  %-10s %s\n", cmd.Name, cmd.Description)
	}
	fmt.Fprintln(a.Out)
	if full {
		fmt.Fprintln(a.Out, "Run `herdlite <command> --help` for command-specific usage.")
	} else {
		fmt.Fprintln(a.Out, "Most setup happens automatically through `herdlite install` and `herdlite link`.")
		fmt.Fprintln(a.Out, "Run `herdlite --helpd` for advanced/internal commands.")
	}
}

func hasHelp(args []string) bool {
	return len(args) > 0 && (args[0] == "-h" || args[0] == "--help")
}
