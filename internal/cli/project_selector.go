package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"herdlite/internal/app"
	"herdlite/internal/state"
)

type projectSelector struct {
	Value    string
	Provided bool
}

func splitProjectSelector(args []string) (projectSelector, []string, error) {
	selector := projectSelector{}
	out := []string{}
	for i := 0; i < len(args); i++ {
		if args[i] != "--p" {
			out = append(out, args[i])
			continue
		}
		if selector.Provided {
			return projectSelector{}, nil, fmt.Errorf("--p was provided more than once")
		}
		if i+1 >= len(args) {
			return projectSelector{}, nil, fmt.Errorf("--p requires a value")
		}
		selector = projectSelector{Value: args[i+1], Provided: true}
		i++
	}
	return selector, out, nil
}

func resolveExistingProject(a *app.App, selector projectSelector) (state.Project, error) {
	if selector.Provided {
		if isPathSelector(selector.Value) {
			project, found, err := a.Store.ProjectForWorkingDirectory(expandHome(selector.Value))
			if err != nil {
				return state.Project{}, err
			}
			if !found {
				return state.Project{}, fmt.Errorf("no linked project found for %s", selector.Value)
			}
			return project, nil
		}

		project, found, err := a.Store.ProjectByNameOrDomain(selector.Value)
		if err != nil {
			return state.Project{}, err
		}
		if !found {
			return state.Project{}, fmt.Errorf("project %s is not linked", selector.Value)
		}
		return project, nil
	}

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

func resolveLinkPath(a *app.App, selector projectSelector) (string, *state.Project, error) {
	if !selector.Provided {
		cwd, err := os.Getwd()
		if err != nil {
			return "", nil, err
		}
		root, err := laravelRoot(cwd)
		if err != nil {
			return "", nil, err
		}
		return root, nil, nil
	}

	if isPathSelector(selector.Value) {
		root, err := laravelRoot(expandHome(selector.Value))
		if err != nil {
			return "", nil, err
		}
		return root, nil, nil
	}

	project, found, err := a.Store.ProjectByNameOrDomain(selector.Value)
	if err != nil {
		return "", nil, err
	}
	if !found {
		return "", nil, fmt.Errorf("project %s is not linked; use --p with a path to create a new link", selector.Value)
	}
	return project.Path, &project, nil
}

func laravelRoot(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		absPath = filepath.Dir(absPath)
	}

	for {
		publicIndex := filepath.Join(absPath, "public", "index.php")
		if _, err := os.Stat(publicIndex); err == nil {
			return absPath, nil
		}
		parent := filepath.Dir(absPath)
		if parent == absPath {
			return "", fmt.Errorf("expected Laravel entrypoint public/index.php at or above %s", path)
		}
		absPath = parent
	}
}

func isPathSelector(value string) bool {
	value = strings.TrimSpace(value)
	return value == "." ||
		value == ".." ||
		strings.HasPrefix(value, "~/") ||
		strings.HasPrefix(value, "/") ||
		strings.Contains(value, "/")
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}
