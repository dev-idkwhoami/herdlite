package install

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type PackageInstaller struct {
	Out io.Writer
	In  io.Reader
}

var pacmanPackages = []string{
	"autoconf",
	"base-devel",
	"bison",
	"ca-certificates",
	"ca-certificates-utils",
	"curl",
	"dnsmasq",
	"icu",
	"libxml2",
	"libzip",
	"libcap",
	"nginx",
	"nss",
	"oniguruma",
	"openssl",
	"pkgconf",
	"postgresql",
	"postgresql-libs",
	"re2c",
	"sqlite",
}

func (p PackageInstaller) PrintPackagePlan() {
	fmt.Fprintln(p.Out, "System packages:")
	for _, pkg := range pacmanPackages {
		fmt.Fprintf(p.Out, "  - %s\n", pkg)
	}
	fmt.Fprintln(p.Out)
	fmt.Fprintln(p.Out, "The latest PHP runtime will be built from source by `herdlite install`.")
	fmt.Fprintln(p.Out, "Additional PHP minors can be installed later with `herdlite php install <minor>`.")
}

func (p PackageInstaller) InstallPacmanPackages(assumeYes bool, dryRun bool) error {
	pacman, err := exec.LookPath("pacman")
	if err != nil {
		return fmt.Errorf("pacman not found")
	}

	args := append([]string{"-S", "--needed"}, pacmanPackages...)
	if assumeYes {
		args = append([]string{"-S", "--needed", "--noconfirm"}, pacmanPackages...)
	}

	fmt.Fprintf(p.Out, "pacman %s\n", strings.Join(args, " "))
	if dryRun {
		return nil
	}

	if !assumeYes && !p.confirm("Install missing/required packages with pacman?") {
		fmt.Fprintln(p.Out, "Skipped package installation.")
		return nil
	}

	cmd := exec.Command(pacman, args...)
	cmd.Stdout = p.Out
	cmd.Stderr = p.Out
	cmd.Stdin = p.In
	return cmd.Run()
}

func (p PackageInstaller) confirm(question string) bool {
	fmt.Fprintf(p.Out, "%s [y/N] ", question)
	scanner := bufio.NewScanner(p.In)
	if !scanner.Scan() {
		return false
	}

	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes"
}
