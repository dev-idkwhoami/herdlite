package cli

import (
	"fmt"
	"os"

	"herdlite/internal/app"
	"herdlite/internal/certs"
	"herdlite/internal/install"
)

func runCert(a *app.App, args []string) int {
	if len(args) == 0 || hasHelp(args) {
		printCertHelp(a)
		return 0
	}

	switch args[0] {
	case "init":
		return runCertInit(a, args[1:])
	case "site":
		return runCertSite(a, args[1:])
	case "trust":
		return runCertTrust(a, args[1:])
	default:
		fmt.Fprintf(a.Err, "unknown cert command: %s\n\n", args[0])
		printCertHelp(a)
		return 1
	}
}

func printCertHelp(a *app.App) {
	fmt.Fprintln(a.Out, "Usage:")
	fmt.Fprintln(a.Out, "  herdlite cert init")
	fmt.Fprintln(a.Out, "  herdlite cert site <domain>")
	fmt.Fprintln(a.Out, "  herdlite cert trust [--dry-run]")
}

func runCertInit(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite cert init")
		return 0
	}
	if len(args) != 0 {
		fmt.Fprintln(a.Err, "Usage: herdlite cert init")
		return 1
	}

	if err := a.InitUserDirs(); err != nil {
		fmt.Fprintf(a.Err, "cert init: %v\n", err)
		return 1
	}

	info, err := certs.Manager{Paths: a.Paths}.EnsureCA()
	if err != nil {
		fmt.Fprintf(a.Err, "cert init: %v\n", err)
		return 1
	}

	if info.Created {
		fmt.Fprintln(a.Out, "Created Herdlite local CA.")
	} else {
		fmt.Fprintln(a.Out, "Herdlite local CA already exists.")
	}
	fmt.Fprintf(a.Out, "  cert:        %s\n", info.CertPath)
	fmt.Fprintf(a.Out, "  key:         %s\n", info.KeyPath)
	fmt.Fprintf(a.Out, "  fingerprint: %s\n", info.Fingerprint)
	return 0
}

func runCertSite(a *app.App, args []string) int {
	if hasHelp(args) || len(args) != 1 {
		fmt.Fprintln(a.Out, "Usage: herdlite cert site <domain>")
		return codeForUsage(args, 1)
	}

	if err := a.InitUserDirs(); err != nil {
		fmt.Fprintf(a.Err, "cert site: %v\n", err)
		return 1
	}

	site, err := certs.Manager{Paths: a.Paths}.EnsureSite(args[0])
	if err != nil {
		fmt.Fprintf(a.Err, "cert site: %v\n", err)
		return 1
	}

	if site.Created {
		fmt.Fprintf(a.Out, "Created certificate for %s.\n", site.Domain)
	} else {
		fmt.Fprintf(a.Out, "Certificate for %s already exists.\n", site.Domain)
	}
	fmt.Fprintf(a.Out, "  cert: %s\n", site.CertPath)
	fmt.Fprintf(a.Out, "  key:  %s\n", site.KeyPath)
	return 0
}

func runCertTrust(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite cert trust [--dry-run]")
		return 0
	}

	dryRun := false
	for _, arg := range args {
		switch arg {
		case "--dry-run":
			dryRun = true
		default:
			fmt.Fprintf(a.Err, "unknown cert trust option: %s\n", arg)
			return 1
		}
	}

	if !dryRun && os.Geteuid() != 0 {
		return rerunWithSudo(a)
	}

	certPaths := a.Paths
	if os.Geteuid() == 0 {
		target, err := install.ResolveTargetUser()
		if err != nil {
			fmt.Fprintf(a.Err, "cert trust: resolve target user: %v\n", err)
			return 1
		}
		certPaths = target.Paths
	}

	info, err := (certs.Manager{Paths: certPaths}).ExistingCA()
	if err != nil {
		fmt.Fprintf(a.Err, "cert trust: %v\n", err)
		return 1
	}

	if err := (install.SystemManager{Out: a.Out}).TrustCA(info.CertPath, dryRun); err != nil {
		fmt.Fprintf(a.Err, "cert trust: %v\n", err)
		return 1
	}
	target, err := install.ResolveTargetUser()
	if err != nil {
		fmt.Fprintf(a.Err, "cert trust: resolve target user: %v\n", err)
		return 1
	}
	if err := (install.SystemManager{Out: a.Out}).TrustUserNSSCA(target, info.CertPath, dryRun); err != nil {
		fmt.Fprintf(a.Err, "cert trust: %v\n", err)
		return 1
	}
	return 0
}
