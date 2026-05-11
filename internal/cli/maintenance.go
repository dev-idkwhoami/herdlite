package cli

import (
	"context"
	"fmt"
	"os"

	"herdlite/internal/app"
	"herdlite/internal/install"
	"herdlite/internal/nginx"
	"herdlite/internal/services"
)

func runRepair(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite repair [--dry-run]")
		return 0
	}

	dryRun := false
	for _, arg := range args {
		switch arg {
		case "--dry-run":
			dryRun = true
		default:
			fmt.Fprintf(a.Err, "unknown repair option: %s\n", arg)
			return 1
		}
	}

	if !dryRun && os.Geteuid() != 0 {
		return rerunWithSudo(a)
	}

	target, err := install.ResolveTargetUser()
	if err != nil {
		fmt.Fprintf(a.Err, "repair: resolve target user: %v\n", err)
		return 1
	}
	if dryRun {
		fmt.Fprintln(a.Out, "Herdlite repair plan:")
		fmt.Fprintln(a.Out, "  - ensure user directories")
		fmt.Fprintln(a.Out, "  - trust local CA in system and browser trust stores")
		fmt.Fprintln(a.Out, "  - enable nginx low-port binding")
		fmt.Fprintln(a.Out, "  - configure wildcard .test DNS")
		fmt.Fprintln(a.Out, "  - render nginx base config")
		fmt.Fprintln(a.Out, "  - write zsh integration")
		fmt.Fprintln(a.Out, "  - append zsh hook at the end of ~/.zshrc if missing")
		return repairSystem(a, target, true)
	}

	if err := install.EnsureTargetDirs(target); err != nil {
		fmt.Fprintf(a.Err, "repair: create target directories: %v\n", err)
		return 1
	}
	if code := repairSystem(a, target, false); code != 0 {
		return code
	}
	fmt.Fprintln(a.Out, "Herdlite system integration repaired.")
	return 0
}

func repairSystem(a *app.App, target install.TargetUser, dryRun bool) int {
	system := install.SystemManager{Out: a.Out}
	caCertPath := target.Paths.CADir + "/herdlite-local-ca.crt"
	if err := system.TrustCA(caCertPath, dryRun); err != nil {
		fmt.Fprintf(a.Err, "repair: local CA trust failed: %v\n", err)
		return 1
	}
	if err := system.TrustUserNSSCA(target, caCertPath, dryRun); err != nil {
		fmt.Fprintf(a.Err, "repair: browser CA trust failed: %v\n", err)
		return 1
	}
	hadCap := true
	if !dryRun {
		var err error
		hadCap, err = system.HasLowPortBinding("")
		if err != nil {
			fmt.Fprintf(a.Err, "repair: nginx capability check failed: %v\n", err)
			return 1
		}
	}
	if err := system.EnableLowPortBinding("", dryRun); err != nil {
		fmt.Fprintf(a.Err, "repair: nginx low-port setup failed: %v\n", err)
		return 1
	}
	if !dryRun && !hadCap {
		if err := setTargetMeta(target, "nginx_cap_added_by_herdlite", "true"); err != nil {
			fmt.Fprintf(a.Err, "repair: record nginx capability ownership: %v\n", err)
			return 1
		}
	}
	if err := system.ConfigureTestDNS(dryRun); err != nil {
		fmt.Fprintf(a.Err, "repair: DNS setup failed: %v\n", err)
		return 1
	}
	if !dryRun {
		nginxConfig, err := (nginx.Manager{Paths: target.Paths}).WriteBaseConfig()
		if err != nil {
			fmt.Fprintf(a.Err, "repair: render nginx config: %v\n", err)
			return 1
		}
		if err := chownTargetPath(target, nginxConfig); err != nil {
			fmt.Fprintf(a.Err, "repair: fix nginx config ownership: %v\n", err)
			return 1
		}
		if os.Geteuid() == 0 {
			if err := install.RestoreTargetOwnership(target); err != nil {
				fmt.Fprintf(a.Err, "repair: fix Herdlite directory ownership: %v\n", err)
				return 1
			}
		}
		fmt.Fprintf(a.Out, "Rendered nginx config: %s\n", nginxConfig)

		executable, err := os.Executable()
		if err != nil {
			fmt.Fprintf(a.Err, "repair: resolve herdlite executable: %v\n", err)
			return 1
		}
		shellIntegration, err := install.WriteZshIntegration(target, executable)
		if err != nil {
			fmt.Fprintf(a.Err, "repair: shell integration failed: %v\n", err)
			return 1
		}
		fmt.Fprintf(a.Out, "Wrote zsh integration: %s\n", shellIntegration.Path)
		if shellIntegration.Appended {
			fmt.Fprintf(a.Out, "Appended zsh hook: %s\n", shellIntegration.ZshrcPath)
		} else {
			fmt.Fprintf(a.Out, "zsh hook already present: %s\n", shellIntegration.ZshrcPath)
		}
	}
	return 0
}

func runUninstall(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite uninstall [--dry-run] [--purge]")
		return 0
	}

	dryRun := false
	purge := false
	for _, arg := range args {
		switch arg {
		case "--dry-run":
			dryRun = true
		case "--purge":
			purge = true
		default:
			fmt.Fprintf(a.Err, "unknown uninstall option: %s\n", arg)
			return 1
		}
	}

	if !dryRun && os.Geteuid() != 0 {
		return rerunWithSudo(a)
	}

	target, err := install.ResolveTargetUser()
	if err != nil {
		fmt.Fprintf(a.Err, "uninstall: resolve target user: %v\n", err)
		return 1
	}

	fmt.Fprintln(a.Out, "Herdlite uninstall plan:")
	fmt.Fprintln(a.Out, "  - stop Herdlite hosting services")
	fmt.Fprintln(a.Out, "  - remove trusted local CA from system and browser trust stores")
	fmt.Fprintln(a.Out, "  - leave nginx low-port capability unchanged")
	fmt.Fprintln(a.Out, "  - comment out wildcard .test DNS config")
	fmt.Fprintln(a.Out, "  - comment out zsh integration and ~/.zshrc hook")
	if purge {
		fmt.Fprintln(a.Out, "  - purge Herdlite config, data, and cache directories")
	}
	if dryRun {
		return uninstallSystem(a, target, purge, true)
	}

	if code := uninstallSystem(a, target, purge, false); code != 0 {
		return code
	}
	fmt.Fprintln(a.Out, "Herdlite system integration removed.")
	if purge {
		fmt.Fprintln(a.Out, "Herdlite config, data, and cache directories were purged.")
	}
	return 0
}

func uninstallSystem(a *app.App, target install.TargetUser, purge bool, dryRun bool) int {
	if !dryRun {
		_ = (services.HostingManager{Paths: target.Paths, Store: a.Store, Out: a.Out}).Stop(context.Background())
	}

	system := install.SystemManager{Out: a.Out}
	if err := system.UntrustCA(dryRun); err != nil {
		fmt.Fprintf(a.Err, "uninstall: local CA removal failed: %v\n", err)
		return 1
	}
	if err := system.UntrustUserNSSCA(target, dryRun); err != nil {
		fmt.Fprintf(a.Err, "uninstall: browser CA removal failed: %v\n", err)
		return 1
	}
	capOwned, _, _ := targetStore(target).MetaValue("nginx_cap_added_by_herdlite")
	if capOwned == "true" {
		if err := system.DisableLowPortBinding("", dryRun); err != nil {
			fmt.Fprintf(a.Err, "uninstall: nginx capability handling failed: %v\n", err)
			return 1
		}
		if !dryRun {
			_ = deleteTargetMeta(target, "nginx_cap_added_by_herdlite")
		}
	} else {
		fmt.Fprintln(a.Out, "Leave nginx low-port capability unchanged; Herdlite did not record ownership.")
	}
	if err := system.DisableTestDNS(dryRun); err != nil {
		fmt.Fprintf(a.Err, "uninstall: DNS disable failed: %v\n", err)
		return 1
	}
	shellPath, err := install.DisableZshIntegration(target, dryRun)
	if err != nil {
		fmt.Fprintf(a.Err, "uninstall: shell integration disable failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.Out, "Comment zsh integration: %s\n", shellPath)

	if purge {
		for _, dir := range []string{target.Paths.ConfigDir, target.Paths.DataDir, target.Paths.CacheDir} {
			fmt.Fprintf(a.Out, "Purge: %s\n", dir)
			if !dryRun {
				if err := os.RemoveAll(dir); err != nil {
					fmt.Fprintf(a.Err, "uninstall: purge %s: %v\n", dir, err)
					return 1
				}
			}
		}
	}
	return 0
}
