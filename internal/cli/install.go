package cli

import (
	"fmt"
	"os"
	"os/exec"

	"herdlite/internal/app"
	"herdlite/internal/install"
	"herdlite/internal/nginx"
	"herdlite/internal/state"
)

func runInstall(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite install [--dry-run] [--yes]")
		return 0
	}

	dryRun := false
	assumeYes := false
	for _, arg := range args {
		switch arg {
		case "--dry-run":
			dryRun = true
		case "--yes", "-y":
			assumeYes = true
		default:
			fmt.Fprintf(a.Err, "unknown install option: %s\n", arg)
			return 1
		}
	}

	if !dryRun && os.Geteuid() != 0 {
		return rerunWithSudo(a)
	}

	target, err := install.ResolveTargetUser()
	if err != nil {
		fmt.Fprintf(a.Err, "install: resolve target user: %v\n", err)
		return 1
	}

	fmt.Fprintln(a.Out, "Herdlite install plan:")
	fmt.Fprintf(a.Out, "  - target user: %s\n", target.Username)
	fmt.Fprintf(a.Out, "  - config dir:  %s\n", target.Paths.ConfigDir)
	fmt.Fprintf(a.Out, "  - data dir:    %s\n", target.Paths.DataDir)
	fmt.Fprintln(a.Out, "  - install required official pacman build/runtime packages")
	fmt.Fprintln(a.Out, "  - resolve latest upstream PHP release")
	fmt.Fprintln(a.Out, "  - download, build, and install latest PHP as default runtime")
	fmt.Fprintln(a.Out, "  - install Herdlite-managed Composer")
	fmt.Fprintln(a.Out, "  - detect DNS resolver and configure wildcard .test")
	fmt.Fprintln(a.Out, "  - create and trust local CA in system and browser trust stores")
	fmt.Fprintln(a.Out, "  - enable user-run nginx to bind ports 80 and 443")
	fmt.Fprintln(a.Out, "  - install shell integration")
	fmt.Fprintln(a.Out)

	packages := install.PackageInstaller{
		Out: a.Out,
		In:  os.Stdin,
	}
	packages.PrintPackagePlan()
	fmt.Fprintln(a.Out)

	if dryRun {
		if err := packages.InstallPacmanPackages(assumeYes, true); err != nil {
			fmt.Fprintf(a.Err, "install: %v\n", err)
			return 1
		}
		system := install.SystemManager{Out: a.Out}
		if err := system.ConfigureTestDNS(true); err != nil {
			fmt.Fprintf(a.Err, "install: %v\n", err)
			return 1
		}
		fmt.Fprintf(a.Out, "Render nginx config: %s\n", target.Paths.NginxDir+"/nginx.conf")
		fmt.Fprintf(a.Out, "Render debug site config: %s\n", target.Paths.NginxSitesDir+"/debug.herdlite.test.conf")
		fmt.Fprintf(a.Out, "Write zsh integration: %s\n", target.Paths.ConfigDir+"/shell/herdlite.zsh")
		fmt.Fprintf(a.Out, "Write PATH shims: %s\n", target.Paths.ShimsDir)
		fmt.Fprintf(a.Out, "Append zsh hook: %s\n", target.HomeDir+"/.zshrc")
		fmt.Fprintf(a.Out, "Install Composer: %s\n", target.Paths.ComposerDir+"/composer.phar")
		fmt.Fprintln(a.Out)
		fmt.Fprintln(a.Out, "Dry run only; no privileged system changes were made.")
		return 0
	}

	if err := install.EnsureTargetDirs(target); err != nil {
		fmt.Fprintf(a.Err, "install: create target directories: %v\n", err)
		return 1
	}

	if err := packages.InstallPacmanPackages(assumeYes, false); err != nil {
		fmt.Fprintf(a.Err, "install: package installation failed: %v\n", err)
		return 1
	}

	if os.Geteuid() == 0 {
		if err := runCertInitAsTargetUser(target.Username); err != nil {
			fmt.Fprintf(a.Err, "install: local CA creation failed: %v\n", err)
			return 1
		}
	}

	system := install.SystemManager{Out: a.Out}
	caCertPath := target.Paths.CADir + "/herdlite-local-ca.crt"
	if err := system.TrustCA(caCertPath, false); err != nil {
		fmt.Fprintf(a.Err, "install: local CA trust failed: %v\n", err)
		return 1
	}
	if err := system.TrustUserNSSCA(target, caCertPath, false); err != nil {
		fmt.Fprintf(a.Err, "install: browser CA trust failed: %v\n", err)
		return 1
	}
	hadCap, err := system.HasLowPortBinding("")
	if err != nil {
		fmt.Fprintf(a.Err, "install: nginx capability check failed: %v\n", err)
		return 1
	}
	if err := system.EnableLowPortBinding("", false); err != nil {
		fmt.Fprintf(a.Err, "install: nginx low-port setup failed: %v\n", err)
		return 1
	}
	if !hadCap {
		if err := setTargetMeta(target, "nginx_cap_added_by_herdlite", "true"); err != nil {
			fmt.Fprintf(a.Err, "install: record nginx capability ownership: %v\n", err)
			return 1
		}
	}
	if err := system.ConfigureTestDNS(false); err != nil {
		fmt.Fprintf(a.Err, "install: DNS setup failed: %v\n", err)
		return 1
	}
	nginxManager := nginx.Manager{Paths: target.Paths}
	nginxConfig, err := nginxManager.WriteBaseConfig()
	if err != nil {
		fmt.Fprintf(a.Err, "install: render nginx config: %v\n", err)
		return 1
	}
	if err := chownTargetPath(target, nginxConfig); err != nil {
		fmt.Fprintf(a.Err, "install: fix nginx config ownership: %v\n", err)
		return 1
	}
	debugConfig, err := nginxManager.WriteDebugSite()
	if err != nil {
		fmt.Fprintf(a.Err, "install: render nginx debug site: %v\n", err)
		return 1
	}
	if err := chownTargetPath(target, debugConfig); err != nil {
		fmt.Fprintf(a.Err, "install: fix nginx debug site ownership: %v\n", err)
		return 1
	}
	if os.Geteuid() == 0 {
		if err := install.RestoreTargetOwnership(target); err != nil {
			fmt.Fprintf(a.Err, "install: fix Herdlite directory ownership: %v\n", err)
			return 1
		}
	}
	fmt.Fprintf(a.Out, "Rendered nginx config: %s\n", nginxConfig)
	fmt.Fprintf(a.Out, "Rendered debug site config: %s\n", debugConfig)

	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintf(a.Err, "install: resolve herdlite executable: %v\n", err)
		return 1
	}
	shellIntegration, err := install.WriteZshIntegration(target, executable)
	if err != nil {
		fmt.Fprintf(a.Err, "install: shell integration failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.Out, "Wrote zsh integration: %s\n", shellIntegration.Path)
	if shellIntegration.Appended {
		fmt.Fprintf(a.Out, "Appended zsh hook: %s\n", shellIntegration.ZshrcPath)
	} else {
		fmt.Fprintf(a.Out, "zsh hook already present: %s\n", shellIntegration.ZshrcPath)
	}

	if os.Geteuid() == 0 {
		if err := runPHPInstallAsTargetUser(target.Username); err != nil {
			fmt.Fprintf(a.Err, "install: latest PHP install failed: %v\n", err)
			return 1
		}
		if err := runComposerInstallAsTargetUser(target.Username); err != nil {
			fmt.Fprintf(a.Err, "install: Composer install failed: %v\n", err)
			return 1
		}
	} else {
		if code := runPHPInstall(a, []string{"latest"}); code != 0 {
			return code
		}
		if code := runComposerInstall(a, nil); code != 0 {
			return code
		}
	}

	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Created Herdlite user directories and handled official pacman dependencies.")
	fmt.Fprintln(a.Out, "Latest PHP runtime, Composer, DNS, local CA trust, nginx low-port binding, and zsh integration are configured.")
	return 0
}

func targetStore(target install.TargetUser) *state.Store {
	return state.NewStore(target.Paths.StateFile)
}

func setTargetMeta(target install.TargetUser, key string, value string) error {
	if err := targetStore(target).SetMetaValue(key, value); err != nil {
		return err
	}
	return chownTargetState(target)
}

func deleteTargetMeta(target install.TargetUser, key string) error {
	if err := targetStore(target).DeleteMetaValue(key); err != nil {
		return err
	}
	return chownTargetState(target)
}

func chownTargetState(target install.TargetUser) error {
	return chownTargetPath(target, target.Paths.StateFile)
}

func chownTargetPath(target install.TargetUser, path string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	if err := os.Chown(path, target.UID, target.GID); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func rerunWithSudo(a *app.App) int {
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		fmt.Fprintln(a.Err, "install requires root and sudo was not found")
		return 1
	}
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintf(a.Err, "resolve herdlite executable: %v\n", err)
		return 1
	}

	args := append([]string{executable}, os.Args[1:]...)
	cmd := exec.Command(sudo, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(a.Err, "sudo failed: %v\n", err)
		return 1
	}

	return 0
}

func runCertInitAsTargetUser(username string) error {
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		return fmt.Errorf("sudo not found")
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve herdlite executable: %w", err)
	}

	args := []string{"-u", username, "-H", executable, "cert", "init"}
	cmd := exec.Command(sudo, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func runPHPInstallAsTargetUser(username string) error {
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		return fmt.Errorf("sudo not found")
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve herdlite executable: %w", err)
	}

	args := []string{"-u", username, "-H", executable, "php", "install", "latest"}
	cmd := exec.Command(sudo, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func runComposerInstallAsTargetUser(username string) error {
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		return fmt.Errorf("sudo not found")
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve herdlite executable: %w", err)
	}

	args := []string{"-u", username, "-H", executable, "composer", "install"}
	cmd := exec.Command(sudo, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
