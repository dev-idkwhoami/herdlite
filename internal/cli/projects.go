package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"herdlite/internal/app"
	"herdlite/internal/certs"
	"herdlite/internal/laravel"
	"herdlite/internal/nginx"
	"herdlite/internal/postgres"
	"herdlite/internal/state"
)

func runLink(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite link [--p <path|name|domain>] [--n <name>] [--d <domain>] [--php <version>] [--ws] [--ws-port <port>]")
		return 0
	}

	selector, args, err := splitProjectSelector(args)
	if err != nil {
		fmt.Fprintf(a.Err, "link: %v\n", err)
		return 1
	}

	requestedPHP := ""
	opts := state.ProjectOptions{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--n":
			if i+1 >= len(args) {
				fmt.Fprintln(a.Err, "link: --n requires a project name")
				return 1
			}
			opts.Name = args[i+1]
			i++
		case "--d":
			if i+1 >= len(args) {
				fmt.Fprintln(a.Err, "link: --d requires a domain")
				return 1
			}
			opts.Domain = args[i+1]
			i++
		case "--php":
			if i+1 >= len(args) {
				fmt.Fprintln(a.Err, "link: --php requires a version")
				return 1
			}
			requestedPHP = args[i+1]
			i++
		case "--ws":
			opts.WebsocketEnabled = true
		case "--ws-port":
			if i+1 >= len(args) {
				fmt.Fprintln(a.Err, "link: --ws-port requires a port")
				return 1
			}
			port, err := strconv.Atoi(args[i+1])
			if err != nil || port < 1 || port > 65535 {
				fmt.Fprintf(a.Err, "link: invalid websocket port %q\n", args[i+1])
				return 1
			}
			opts.WebsocketPort = port
			opts.WebsocketEnabled = true
			i++
		default:
			fmt.Fprintf(a.Err, "unknown link option: %s\n", args[i])
			return 1
		}
	}

	if err := a.InitUserDirs(); err != nil {
		fmt.Fprintf(a.Err, "link: %v\n", err)
		return 1
	}

	projectPath, existingProject, err := resolveLinkPath(a, selector)
	if err != nil {
		fmt.Fprintf(a.Err, "link: %v\n", err)
		return 1
	}
	if existingProject == nil {
		if project, found, err := a.Store.ProjectByPath(projectPath); err != nil {
			fmt.Fprintf(a.Err, "link: %v\n", err)
			return 1
		} else if found {
			existingProject = &project
		}
	}
	oldProject := existingProject

	runtime, err := resolveLinkPHPRuntime(a, existingProject, projectPath, requestedPHP)
	if err != nil {
		fmt.Fprintf(a.Err, "link: %v\n", err)
		return 1
	}
	opts.PHPVersion = runtime.Version

	project, err := a.Store.AddProjectWithOptions(projectPath, opts)
	if err != nil {
		fmt.Fprintf(a.Err, "link: %v\n", err)
		return 1
	}

	cert, err := (certs.Manager{Paths: a.Paths}).EnsureSite(project.Domain)
	if err != nil {
		fmt.Fprintf(a.Err, "link: site cert: %v\n", err)
		return 1
	}
	nginxManager := nginx.Manager{Paths: a.Paths}
	siteConf, err := nginxManager.WriteSite(project, runtime, cert)
	if err != nil {
		fmt.Fprintf(a.Err, "link: nginx site config: %v\n", err)
		return 1
	}
	if oldProject != nil && oldProject.Domain != project.Domain {
		removeStaleSiteConfigs(a.Paths.NginxSitesDir, *oldProject)
	}
	if _, err := nginxManager.WriteBaseConfig(); err != nil {
		fmt.Fprintf(a.Err, "link: nginx base config: %v\n", err)
		return 1
	}

	database := projectDatabaseName(project)
	if err := (postgres.Manager{Paths: a.Paths, Out: a.Out}).EnsureDatabase(context.Background(), database); err != nil {
		fmt.Fprintf(a.Err, "link: create project database: %v\n", err)
		return 1
	}
	if err := laravel.ApplyEnv(project.Path, laravel.DatabaseEnv(database)); err != nil {
		fmt.Fprintf(a.Err, "link: update .env database values: %v\n", err)
		return 1
	}
	if err := laravel.ApplyEnv(project.Path, laravel.MailEnv("noreply@"+project.Domain)); err != nil {
		fmt.Fprintf(a.Err, "link: update .env mail values: %v\n", err)
		return 1
	}

	fmt.Fprintf(a.Out, "Linked %s\n", project.Name)
	fmt.Fprintf(a.Out, "  path:   %s\n", project.Path)
	fmt.Fprintf(a.Out, "  domain: https://%s\n", project.Domain)
	fmt.Fprintf(a.Out, "  php:    %s\n", runtime.Version)
	fmt.Fprintf(a.Out, "  db:     %s\n", database)
	fmt.Fprintf(a.Out, "  mail:   noreply@%s -> 127.0.0.1:1025\n", project.Domain)
	fmt.Fprintf(a.Out, "  config: %s\n", siteConf)

	if project.Websocket.Enabled {
		wsCert, err := (certs.Manager{Paths: a.Paths}).EnsureSite(project.Websocket.Domain)
		if err != nil {
			fmt.Fprintf(a.Err, "link: websocket cert: %v\n", err)
			return 1
		}
		wsConf, err := (nginx.Manager{Paths: a.Paths}).WriteWebsocket(project, wsCert)
		if err != nil {
			fmt.Fprintf(a.Err, "link: websocket nginx config: %v\n", err)
			return 1
		}
		if err := laravel.ApplyEnv(project.Path, laravel.WebsocketEnv(project.Websocket.Domain)); err != nil {
			fmt.Fprintf(a.Err, "link: update .env websocket values: %v\n", err)
			return 1
		}
		fmt.Fprintf(a.Out, "  websocket: https://%s -> 127.0.0.1:%d\n", project.Websocket.Domain, project.Websocket.Port)
		fmt.Fprintf(a.Out, "  ws config: %s\n", wsConf)
		fmt.Fprintf(a.Out, "  env:      %s\n", project.Path+"/.env")
	}
	return 0
}

func removeStaleSiteConfigs(sitesDir string, project state.Project) {
	_ = os.Remove(sitesDir + "/" + project.Domain + ".conf")
	_ = os.Remove(sitesDir + "/" + project.Domain + ".ws.conf")
}

func projectDatabaseName(project state.Project) string {
	name := strings.ToLower(project.Name)
	var b strings.Builder
	lastUnderscore := false
	for _, r := range name {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if valid {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "project"
	}
	return out
}

func resolveLinkPHPRuntime(a *app.App, existingProject *state.Project, projectPath string, requested string) (state.PHPRuntime, error) {
	if requested == "" {
		existing := existingProject
		if existing == nil {
			project, found, err := a.Store.ProjectByPath(projectPath)
			if err != nil {
				return state.PHPRuntime{}, err
			}
			if found {
				existing = &project
			}
		}
		if existing != nil && existing.PHPVersion != "" {
			runtime, ok, err := a.Store.PHPRuntimeForRequest(existing.PHPVersion)
			if err != nil {
				return state.PHPRuntime{}, err
			}
			if ok {
				return runtime, nil
			}
			return state.PHPRuntime{}, fmt.Errorf("PHP %s for existing project is not installed", existing.PHPVersion)
		}
	}

	runtime, found, err := a.Store.PHPRuntimeForRequest(requested)
	if err != nil {
		return state.PHPRuntime{}, err
	}
	if found {
		return runtime, nil
	}
	if requested == "" {
		return state.PHPRuntime{}, fmt.Errorf("no PHP runtimes installed; run `herdlite php install latest` first")
	}
	return state.PHPRuntime{}, fmt.Errorf("PHP %s is not installed; run `herdlite php install %s` first", requested, requested)
}

func runList(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite list")
		return 0
	}

	projects, err := a.Store.Projects()
	if err != nil {
		fmt.Fprintf(a.Err, "list: %v\n", err)
		return 1
	}

	if len(projects) == 0 {
		fmt.Fprintln(a.Out, "No projects linked yet.")
		return 0
	}

	w := tabwriter.NewWriter(a.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tDOMAIN\tPHP\tWS\tPATH")
	for _, project := range projects {
		phpVersion := project.PHPVersion
		if phpVersion == "" {
			phpVersion = "-"
		}
		ws := "-"
		if project.Websocket.Enabled {
			ws = project.Websocket.Domain
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", project.Name, project.Domain, phpVersion, ws, project.Path)
	}
	w.Flush()
	return 0
}

func codeForUsage(args []string, want int) int {
	if hasHelp(args) {
		return 0
	}
	if len(args) != want {
		return 1
	}
	return 0
}
