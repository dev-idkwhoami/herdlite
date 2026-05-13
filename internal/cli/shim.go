package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"herdlite/internal/app"
	"herdlite/internal/composer"
	"herdlite/internal/state"
)

func runShim(a *app.App, args []string) int {
	if len(args) == 0 || hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite shim <php|composer|node|npm|npx> [args...]")
		return codeForUsage(args, 1)
	}

	name := args[0]
	rest := args[1:]

	switch name {
	case "php":
		_, runtime, err := resolveShimPHPRuntime(a)
		if err != nil {
			fmt.Fprintf(a.Err, "shim php: %v\n", err)
			return 1
		}
		return runExternal(runtime.PHPBinary, rest...)
	case "composer":
		project, runtime, err := resolveShimPHPRuntime(a)
		if err != nil {
			fmt.Fprintf(a.Err, "shim composer: %v\n", err)
			return 1
		}
		dir := ""
		if project != nil {
			dir = project.Path
			projectComposer := filepath.Join(project.Path, "composer.phar")
			if _, err := os.Stat(projectComposer); err == nil {
				return runExternal(runtime.PHPBinary, append([]string{projectComposer}, rest...)...)
			}
		}
		managedComposer := (composer.Manager{Paths: a.Paths}).Path()
		if _, err := os.Stat(managedComposer); err != nil {
			fmt.Fprintln(a.Err, "shim composer: Herdlite Composer is not installed; run `herdlite composer install`")
			return 1
		}
		return runExternalWithEnv(dir, os.Environ(), runtime.PHPBinary, append([]string{managedComposer}, rest...)...)
	case "node", "npm", "npx":
		project, err := currentProjectOptional(a)
		if err != nil {
			fmt.Fprintf(a.Err, "shim %s: %v\n", name, err)
			return 1
		}
		globalNode, _, err := a.Store.MetaValue("global_node_version")
		if err != nil {
			fmt.Fprintf(a.Err, "shim %s: %v\n", name, err)
			return 1
		}
		return runNVMCommand(project, globalNode, a.Paths.ShimsDir, name, rest...)
	default:
		fmt.Fprintf(a.Err, "unknown shim command: %s\n", name)
		return 1
	}
}

func currentProject(a *app.App) (state.Project, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return state.Project{}, err
	}
	project, found, err := a.Store.ProjectForWorkingDirectory(cwd)
	if err != nil {
		return state.Project{}, err
	}
	if !found {
		return state.Project{}, fmt.Errorf("current directory is not inside a linked project")
	}
	return project, nil
}

func currentProjectOptional(a *app.App) (*state.Project, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	project, found, err := a.Store.ProjectForWorkingDirectory(cwd)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return &project, nil
}

func resolveShimPHPRuntime(a *app.App) (*state.Project, state.PHPRuntime, error) {
	project, err := currentProjectOptional(a)
	if err != nil {
		return nil, state.PHPRuntime{}, err
	}
	if project != nil && project.PHPVersion != "" {
		runtime, found, err := a.Store.PHPRuntimeForRequest(project.PHPVersion)
		if err != nil {
			return nil, state.PHPRuntime{}, err
		}
		if !found {
			return nil, state.PHPRuntime{}, fmt.Errorf("PHP %s for project %s is not installed", project.PHPVersion, project.Name)
		}
		return project, runtime, nil
	}

	if version, ok, err := a.Store.MetaValue("global_php_version"); err != nil {
		return nil, state.PHPRuntime{}, err
	} else if ok && version != "" {
		runtime, found, err := a.Store.PHPRuntimeForRequest(version)
		if err != nil {
			return nil, state.PHPRuntime{}, err
		}
		if !found {
			return nil, state.PHPRuntime{}, fmt.Errorf("global PHP %s is not installed", version)
		}
		return nil, runtime, nil
	}

	runtime, found, err := a.Store.LatestPHPRuntime()
	if err != nil {
		return nil, state.PHPRuntime{}, err
	}
	if !found {
		return nil, state.PHPRuntime{}, fmt.Errorf("no PHP runtime installed; run `herdlite php install latest` first")
	}
	return nil, runtime, nil
}

func currentProjectRuntime(a *app.App) (state.Project, state.PHPRuntime, error) {
	project, err := currentProject(a)
	if err != nil {
		return state.Project{}, state.PHPRuntime{}, err
	}
	if project.PHPVersion == "" {
		return state.Project{}, state.PHPRuntime{}, fmt.Errorf("project %s has no selected PHP version", project.Name)
	}

	runtime, found, err := a.Store.PHPRuntimeForRequest(project.PHPVersion)
	if err != nil {
		return state.Project{}, state.PHPRuntime{}, err
	}
	if !found {
		return state.Project{}, state.PHPRuntime{}, fmt.Errorf("PHP %s for project %s is not installed", project.PHPVersion, project.Name)
	}
	return project, runtime, nil
}

func runNVMCommand(project *state.Project, globalVersion string, shimsDir string, name string, args ...string) int {
	nvmDir := os.Getenv("NVM_DIR")
	if nvmDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "shim %s: resolve home: %v\n", name, err)
			return 1
		}
		nvmDir = filepath.Join(home, ".nvm")
	}
	nvmScript := filepath.Join(nvmDir, "nvm.sh")
	if _, err := os.Stat(nvmScript); err != nil {
		fmt.Fprintf(os.Stderr, "shim %s: nvm.sh not found at %s\n", name, nvmScript)
		return 1
	}

	script := `
source "$NVM_DIR/nvm.sh"
if [ -n "$HERDLITE_PROJECT_PATH" ]; then
  cd "$HERDLITE_PROJECT_PATH"
fi
if [ -f .nvmrc ]; then
  nvm install --silent
  nvm use --silent
elif [ -n "$HERDLITE_NODE_VERSION" ]; then
  nvm install --silent "$HERDLITE_NODE_VERSION"
  nvm use --silent "$HERDLITE_NODE_VERSION"
fi
command "$HERDLITE_NODE_COMMAND" "$@"
`
	projectPath := ""
	if project != nil {
		projectPath = project.Path
	}
	cmd := exec.Command("zsh", append([]string{"-lc", script, "herdlite-shim"}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	env := os.Environ()
	if shimsDir != "" {
		env = append(env, "PATH="+pathWithoutDir(os.Getenv("PATH"), shimsDir))
	}
	cmd.Env = append(env,
		"NVM_DIR="+nvmDir,
		"HERDLITE_PROJECT_PATH="+projectPath,
		"HERDLITE_NODE_VERSION="+globalVersion,
		"HERDLITE_NODE_COMMAND="+name,
	)
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "shim %s: %v\n", name, err)
		return 1
	}
	return 0
}

func pathWithoutDir(pathValue string, dir string) string {
	entries := filepath.SplitList(pathValue)
	filtered := entries[:0]
	cleanDir := filepath.Clean(dir)
	for _, entry := range entries {
		if filepath.Clean(entry) == cleanDir {
			continue
		}
		filtered = append(filtered, entry)
	}
	return strings.Join(filtered, string(os.PathListSeparator))
}

func runExternal(name string, args ...string) int {
	return runExternalWithEnv("", os.Environ(), name, args...)
}

func runExternalWithEnv(dir string, env []string, name string, args ...string) int {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = env
	if dir == "" {
		cmd.Dir, _ = os.Getwd()
	} else {
		cmd.Dir = dir
	}

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		return 1
	}
	return 0
}

func prependPath(env []string, dir string) []string {
	out := make([]string, 0, len(env)+1)
	found := false
	for _, item := range env {
		if strings.HasPrefix(item, "PATH=") {
			out = append(out, "PATH="+dir+":"+strings.TrimPrefix(item, "PATH="))
			found = true
			continue
		}
		out = append(out, item)
	}
	if !found {
		out = append(out, "PATH="+dir)
	}
	return out
}
