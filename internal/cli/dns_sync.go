package cli

import (
	"fmt"
	"os"
	"os/exec"

	"herdlite/internal/app"
	"herdlite/internal/dns"
	"herdlite/internal/install"
)

func runDNSSync(a *app.App, args []string) int {
	if hasHelp(args) || len(args) != 0 {
		fmt.Fprintln(a.Out, "Usage: herdlite dns-sync")
		return codeForUsage(args, 0)
	}
	if os.Geteuid() != 0 {
		return runDNSSyncWithSudo(a)
	}
	if err := syncSystemDNS(a); err != nil {
		fmt.Fprintf(a.Err, "dns-sync: %v\n", err)
		return 1
	}
	fmt.Fprintln(a.Out, "Updated system DNS records.")
	return 0
}

func runDNSSyncWithSudo(a *app.App) int {
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		fmt.Fprintln(a.Err, "dns-sync requires root and sudo was not found")
		return 1
	}
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintf(a.Err, "resolve herdlite executable: %v\n", err)
		return 1
	}

	cmd := exec.Command(sudo, executable, "dns-sync")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(a.Err, "sudo dns-sync failed: %v\n", err)
		return 1
	}
	return 0
}

func syncSystemDNS(a *app.App) error {
	target, err := install.ResolveTargetUser()
	if err != nil {
		return fmt.Errorf("resolve target user: %w", err)
	}
	config, err := dns.HostRecordConfig(targetStore(target))
	if err != nil {
		return fmt.Errorf("build DNS records: %w", err)
	}
	if err := (install.SystemManager{Out: a.Out}).ConfigureTestDNS(config, false); err != nil {
		return err
	}
	return nil
}
