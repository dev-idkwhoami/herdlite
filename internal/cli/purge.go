package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"herdlite/internal/app"
	"herdlite/internal/debugui"
	"herdlite/internal/postgres"
	"herdlite/internal/state"
)

func runPurge(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite purge [--dry-run]")
		return 0
	}

	dryRun := false
	for _, arg := range args {
		switch arg {
		case "--dry-run":
			dryRun = true
		default:
			fmt.Fprintf(a.Err, "unknown purge option: %s\n", arg)
			return 1
		}
	}

	projects, err := a.Store.Projects()
	if err != nil {
		fmt.Fprintf(a.Err, "purge: %v\n", err)
		return 1
	}
	activeDomains := map[string]bool{debugui.Domain: true}
	for _, project := range projects {
		activeDomains[project.Domain] = true
		if project.Websocket.Enabled {
			activeDomains[project.Websocket.Domain] = true
		}
	}

	certFiles, err := unusedSiteCertFiles(a.Paths.SiteCertDir, activeDomains)
	if err != nil {
		fmt.Fprintf(a.Err, "purge: scan certs: %v\n", err)
		return 1
	}
	for _, path := range certFiles {
		fmt.Fprintf(a.Out, "Remove unused cert file: %s\n", path)
		if !dryRun {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(a.Err, "purge: remove %s: %v\n", path, err)
				return 1
			}
		}
	}

	resources, err := a.Store.ProjectResources()
	if err != nil {
		fmt.Fprintf(a.Err, "purge: project resources: %v\n", err)
		return 1
	}
	pg := postgres.Manager{Paths: a.Paths, Out: a.Out}
	for _, resource := range resources {
		if resource.Linked || resource.DatabaseName == "" {
			continue
		}
		fmt.Fprintf(a.Out, "Drop unused database: %s\n", resource.DatabaseName)
		if !dryRun {
			if err := pg.DropDatabase(context.Background(), resource.DatabaseName); err != nil {
				fmt.Fprintf(a.Err, "purge: drop database %s: %v\n", resource.DatabaseName, err)
				return 1
			}
			if err := a.Store.ForgetProjectResource(resource.ProjectName); err != nil {
				fmt.Fprintf(a.Err, "purge: forget resource %s: %v\n", resource.ProjectName, err)
				return 1
			}
		}
	}

	if len(certFiles) == 0 {
		fmt.Fprintln(a.Out, "No unused cert files found.")
	}
	if !hasUnusedDatabase(resources) {
		fmt.Fprintln(a.Out, "No unused Herdlite-recorded databases found.")
	}
	if dryRun {
		fmt.Fprintln(a.Out, "Would update system DNS records.")
	} else if code := runDNSSyncWithSudo(a); code != 0 {
		return code
	}
	fmt.Fprintln(a.Out, "Project folders were not touched.")
	return 0
}

func unusedSiteCertFiles(siteCertDir string, activeDomains map[string]bool) ([]string, error) {
	entries, err := os.ReadDir(siteCertDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := filepath.Ext(name)
		if ext != ".crt" && ext != ".key" {
			continue
		}
		domain := strings.TrimSuffix(name, ext)
		if activeDomains[domain] {
			continue
		}
		out = append(out, filepath.Join(siteCertDir, name))
	}
	return out, nil
}

func hasUnusedDatabase(resources []state.ProjectResource) bool {
	for _, resource := range resources {
		if !resource.Linked && resource.DatabaseName != "" {
			return true
		}
	}
	return false
}
