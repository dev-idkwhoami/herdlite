package phpmanager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"herdlite/internal/certs"
	"herdlite/internal/nginx"
	"herdlite/internal/state"
)

type RemovalPlan struct {
	Before            state.PHPUpdateState
	Choices           []state.PHPRuntime
	Replacements      map[string]string // project name -> exact target patch
	GlobalReplacement string
}

func (i Installer) PlanRemoval(version string) (RemovalPlan, error) {
	p := RemovalPlan{Replacements: map[string]string{}}
	if !isFullVersion(version) {
		return p, fmt.Errorf("remove requires an exact installed patch, such as 8.5.6")
	}
	runtimes, err := i.Store.PHPRuntimes()
	if err != nil {
		return p, err
	}
	for _, r := range runtimes {
		if r.Version == version {
			p.Before.Old = []state.PHPRuntime{r}
		} else {
			p.Choices = append(p.Choices, r)
		}
	}
	if len(p.Before.Old) == 0 {
		return p, fmt.Errorf("PHP %s is not installed", version)
	}
	locked, err := i.Store.PHPLocked(version)
	if err != nil {
		return p, err
	}
	if locked {
		return p, fmt.Errorf("PHP %s is locked; run `herdlite php unlock %s` first", version, version)
	}
	r := p.Before.Old[0]
	if filepath.Clean(r.Prefix) != filepath.Join(i.Paths.PHPDir, r.Version) {
		return p, fmt.Errorf("refusing to remove unmanaged prefix %s", r.Prefix)
	}
	info, err := os.Lstat(r.Prefix)
	if err != nil {
		return p, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return p, fmt.Errorf("runtime prefix is not a regular directory: %s", r.Prefix)
	}
	projects, err := i.Store.Projects()
	if err != nil {
		return p, err
	}
	for _, project := range projects {
		if project.PHPVersion == version {
			p.Before.Projects = append(p.Before.Projects, project)
		}
	}
	p.Before.Global, p.Before.HasGlobal, err = i.Store.MetaValue("global_php_version")
	if err != nil {
		return p, err
	}
	if len(p.Choices) == 0 && (len(p.Before.Projects) > 0 || p.Before.Global == version) {
		return p, fmt.Errorf("PHP %s is still in use and no replacement is installed; install another version first", version)
	}
	return p, nil
}

func (p RemovalPlan) Target(version string) (state.PHPRuntime, error) {
	for _, r := range p.Choices {
		if r.Version == version {
			return r, nil
		}
	}
	return state.PHPRuntime{}, fmt.Errorf("PHP %s is not an available replacement", version)
}

func (i Installer) ValidateRemoval(ctx context.Context, p RemovalPlan) error {
	seen := map[string]bool{}
	targets := []string{}
	for _, project := range p.Before.Projects {
		targets = append(targets, p.Replacements[project.Name])
	}
	if p.Before.HasGlobal && p.Before.Global == p.Before.Old[0].Version {
		targets = append(targets, p.GlobalReplacement)
	}
	for _, version := range targets {
		if seen[version] {
			continue
		}
		seen[version] = true
		r, err := p.Target(version)
		if err != nil {
			return err
		}
		if err := ValidateRuntimeContext(ctx, r); err != nil {
			return fmt.Errorf("replacement PHP %s: %w", version, err)
		}
	}
	return nil
}

// ApplyRemoval retains the installation directory until service restoration succeeds.
// A non-nil result on error signals incomplete rollback, as with ApplyUpdate.
func (i Installer) ApplyRemoval(ctx context.Context, p RemovalPlan) (result *UpdateResult, err error) {
	if err = i.ValidateRemoval(ctx, p); err != nil {
		return nil, err
	}
	r := &UpdateResult{installer: i, before: p.Before, Plan: UpdatePlan{Old: p.Before.Old}}
	defer func() {
		if err != nil {
			if recovery := r.Rollback(); recovery != nil {
				result = r
				err = errors.Join(err, fmt.Errorf("ROLLBACK FAILED: %w", recovery))
			} else {
				result = nil
			}
		}
	}()
	manager := nginx.Manager{Paths: i.Paths}
	for _, project := range p.Before.Projects {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		target, e := p.Target(p.Replacements[project.Name])
		if e != nil {
			return nil, e
		}
		file, e := saveFile(manager.SiteConfigPath(project))
		if e != nil {
			return nil, e
		}
		r.files = append(r.files, file)
		if !project.Enabled {
			if e = os.Remove(file.path); e != nil && !os.IsNotExist(e) {
				return nil, e
			}
			continue
		}
		cert, e := (certs.Manager{Paths: i.Paths}).EnsureSite(project.Domain)
		if e != nil {
			return nil, e
		}
		if _, err = manager.WriteSite(project, target, cert); err != nil {
			return nil, err
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = i.Store.ApplyPHPRemoval(p.Before, p.Replacements, p.GlobalReplacement); err != nil {
		return nil, err
	}
	r.switched = true
	return r, nil
}
