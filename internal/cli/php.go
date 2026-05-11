package cli

import (
	"context"
	"fmt"
	"text/tabwriter"

	"herdlite/internal/app"
	"herdlite/internal/certs"
	"herdlite/internal/nginx"
	"herdlite/internal/phpmanager"
)

func runPHP(a *app.App, args []string) int {
	if len(args) == 0 || hasHelp(args) {
		printPHPHelp(a)
		return 0
	}

	switch args[0] {
	case "available":
		return runPHPAvailable(a, args[1:])
	case "eol":
		return runPHPEOL(a, args[1:])
	case "install":
		return runPHPInstall(a, args[1:])
	case "list":
		return runPHPList(a, args[1:])
	case "global":
		return runPHPGlobal(a, args[1:])
	case "use":
		return runPHPUse(a, args[1:])
	default:
		fmt.Fprintf(a.Err, "unknown php command: %s\n\n", args[0])
		printPHPHelp(a)
		return 1
	}
}

func printPHPHelp(a *app.App) {
	fmt.Fprintln(a.Out, "Usage:")
	fmt.Fprintln(a.Out, "  herdlite php available")
	fmt.Fprintln(a.Out, "  herdlite php eol sync")
	fmt.Fprintln(a.Out, "  herdlite php install [latest|8.5|8.5.6] [--dry-run] [--force] [--keep-build] [--verbose]")
	fmt.Fprintln(a.Out, "  herdlite php list")
	fmt.Fprintln(a.Out, "  herdlite php global <version>")
	fmt.Fprintln(a.Out, "  herdlite php use <version> [--p <path|name|domain>]")
}

func runPHPAvailable(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite php available")
		return 0
	}

	releases, err := (phpmanager.ReleaseClient{}).Releases(context.Background())
	if err != nil {
		fmt.Fprintf(a.Err, "php available: %v\n", err)
		return 1
	}

	latest := phpmanager.LatestByMinor(releases)
	if len(latest) == 0 {
		fmt.Fprintln(a.Out, "No PHP releases found.")
		return 0
	}

	eol, _ := a.Store.PHPEOLMap()
	w := tabwriter.NewWriter(a.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "MINOR\tLATEST\tSTATUS\tTAG\tPUBLISHED")
	for _, release := range latest {
		status := "supported"
		if branch, ok := eol[release.Minor]; ok {
			status = "EOL " + branch.EOLDate
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", release.Minor, release.Version, status, release.Tag, release.Published.Format("2006-01-02"))
	}
	w.Flush()
	return 0
}

func runPHPEOL(a *app.App, args []string) int {
	if len(args) == 0 || hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite php eol sync")
		return 0
	}

	switch args[0] {
	case "sync":
		if len(args) > 1 {
			fmt.Fprintln(a.Err, "Usage: herdlite php eol sync")
			return 1
		}
		if err := a.InitUserDirs(); err != nil {
			fmt.Fprintf(a.Err, "php eol sync: %v\n", err)
			return 1
		}
		branches, err := phpmanager.SyncEOL(context.Background(), a.Store)
		if err != nil {
			fmt.Fprintf(a.Err, "php eol sync: %v\n", err)
			return 1
		}
		fmt.Fprintf(a.Out, "Synced %d EOL PHP branches.\n", len(branches))
		return 0
	default:
		fmt.Fprintf(a.Err, "unknown php eol command: %s\n", args[0])
		return 1
	}
}

func runPHPInstall(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite php install [latest|8.5|8.5.6] [--dry-run] [--force] [--keep-build] [--verbose]")
		return 0
	}

	requested := "latest"
	dryRun := false
	force := false
	keepBuild := false
	verbose := false
	seenVersion := false

	for _, arg := range args {
		switch arg {
		case "--dry-run":
			dryRun = true
		case "--force":
			force = true
		case "--keep-build":
			keepBuild = true
		case "--verbose":
			verbose = true
		default:
			if seenVersion {
				fmt.Fprintf(a.Err, "php install accepts only one version argument\n")
				return 1
			}
			requested = arg
			seenVersion = true
		}
	}

	if !dryRun {
		if err := a.InitUserDirs(); err != nil {
			fmt.Fprintf(a.Err, "php install: %v\n", err)
			return 1
		}
	}

	installer := phpmanager.Installer{
		Paths: a.Paths,
		Store: a.Store,
		Out:   a.Out,
		Err:   a.Err,
	}

	runtime, err := installer.Install(context.Background(), phpmanager.InstallOptions{
		Requested: requested,
		DryRun:    dryRun,
		Force:     force,
		KeepBuild: keepBuild,
		Verbose:   verbose,
	})
	if err != nil {
		fmt.Fprintf(a.Err, "php install: %v\n", err)
		return 1
	}

	if dryRun {
		return 0
	}

	fmt.Fprintf(a.Out, "Installed PHP %s\n", runtime.Version)
	fmt.Fprintf(a.Out, "  php:     %s\n", runtime.PHPBinary)
	fmt.Fprintf(a.Out, "  php-fpm: %s\n", runtime.PHPFPMBinary)
	return 0
}

func runPHPList(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite php list")
		return 0
	}

	runtimes, err := a.Store.PHPRuntimes()
	if err != nil {
		fmt.Fprintf(a.Err, "php list: %v\n", err)
		return 1
	}

	if len(runtimes) == 0 {
		fmt.Fprintln(a.Out, "No PHP runtimes installed yet.")
		return 0
	}

	eol, _ := a.Store.PHPEOLMap()
	w := tabwriter.NewWriter(a.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "VERSION\tMINOR\tSTATUS\tSOURCE\tPREFIX")
	for _, runtime := range runtimes {
		status := "supported"
		if branch, ok := eol[runtime.Minor]; ok {
			status = "EOL " + branch.EOLDate
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", runtime.Version, runtime.Minor, status, runtime.Source, runtime.Prefix)
	}
	w.Flush()
	return 0
}

func runPHPUse(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite php use <version> [--p <path|name|domain>]")
		return 0
	}

	selector, args, err := splitProjectSelector(args)
	if err != nil {
		fmt.Fprintf(a.Err, "php use: %v\n", err)
		return 1
	}
	if len(args) != 1 {
		fmt.Fprintln(a.Err, "Usage: herdlite php use <version> [--p <path|name|domain>]")
		return 1
	}

	requested := args[0]
	if err := a.InitUserDirs(); err != nil {
		fmt.Fprintf(a.Err, "php use: %v\n", err)
		return 1
	}

	runtime, found, err := a.Store.PHPRuntimeForRequest(requested)
	if err != nil {
		fmt.Fprintf(a.Err, "php use: %v\n", err)
		return 1
	}
	if !found {
		fmt.Fprintf(a.Err, "php use: PHP %s is not installed; run `herdlite php install %s` first\n", requested, requested)
		return 1
	}

	project, err := resolveExistingProject(a, selector)
	if err != nil {
		fmt.Fprintf(a.Err, "php use: %v\n", err)
		return 1
	}

	project, err = a.Store.SetProjectPHPVersion(project.Name, runtime.Version)
	if err != nil {
		fmt.Fprintf(a.Err, "php use: %v\n", err)
		return 1
	}

	cert, err := (certs.Manager{Paths: a.Paths}).EnsureSite(project.Domain)
	if err != nil {
		fmt.Fprintf(a.Err, "php use: site cert: %v\n", err)
		return 1
	}
	siteConf, err := (nginx.Manager{Paths: a.Paths}).WriteSite(project, runtime, cert)
	if err != nil {
		fmt.Fprintf(a.Err, "php use: nginx site config: %v\n", err)
		return 1
	}

	fmt.Fprintf(a.Out, "Pinned %s to PHP %s\n", project.Name, runtime.Version)
	fmt.Fprintf(a.Out, "  path:   %s\n", project.Path)
	fmt.Fprintf(a.Out, "  domain: https://%s\n", project.Domain)
	fmt.Fprintf(a.Out, "  config: %s\n", siteConf)
	fmt.Fprintln(a.Out, "Run `herdlite start` to start or reload hosting services.")
	return 0
}

func runPHPGlobal(a *app.App, args []string) int {
	if hasHelp(args) || len(args) != 1 {
		fmt.Fprintln(a.Out, "Usage: herdlite php global <version>")
		return codeForUsage(args, 1)
	}
	requested := args[0]
	if err := a.InitUserDirs(); err != nil {
		fmt.Fprintf(a.Err, "php global: %v\n", err)
		return 1
	}
	runtime, found, err := a.Store.PHPRuntimeForRequest(requested)
	if err != nil {
		fmt.Fprintf(a.Err, "php global: %v\n", err)
		return 1
	}
	if !found {
		fmt.Fprintf(a.Err, "php global: PHP %s is not installed; run `herdlite php install %s` first\n", requested, requested)
		return 1
	}
	if err := a.Store.SetMetaValue("global_php_version", runtime.Version); err != nil {
		fmt.Fprintf(a.Err, "php global: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.Out, "Global PHP set to %s\n", runtime.Version)
	return 0
}
