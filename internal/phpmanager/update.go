package phpmanager

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"herdlite/internal/certs"
	"herdlite/internal/nginx"
	"herdlite/internal/state"
)

type UpdatePlan struct {
	Minor      string
	Release    Release
	Old        []state.PHPRuntime // Unlocked installations only.
	Locked     []state.PHPRuntime
	Target     *state.PHPRuntime
	Projects   []state.Project
	Global     bool
	Force      bool
	Extensions []Extension
	Problem    string
}

func (p UpdatePlan) Needed() bool {
	if len(p.Old) == 0 {
		return false
	}
	if p.Force {
		return true
	}
	for _, old := range p.Old {
		if old.Version != p.Release.Version {
			return true
		}
	}
	return false
}

// SelectUpdates groups installed patches, never widening to an uninstalled minor.
func SelectUpdates(runtimes []state.PHPRuntime, locked map[string]bool, releases []Release, minor string, force bool) ([]UpdatePlan, error) {
	if minor != "" && !isMinorVersion(minor) {
		return nil, fmt.Errorf("use a minor version such as 8.5; locks use exact patches")
	}
	groups := map[string]*UpdatePlan{}
	for _, r := range runtimes {
		if minor != "" && r.Minor != minor {
			continue
		}
		p := groups[r.Minor]
		if p == nil {
			p = &UpdatePlan{Minor: r.Minor, Force: force}
			groups[r.Minor] = p
		}
		if locked[r.Version] {
			p.Locked = append(p.Locked, r)
		} else {
			p.Old = append(p.Old, r)
		}
	}
	if minor != "" && len(groups) == 0 {
		return nil, fmt.Errorf("PHP %s is not installed", minor)
	}
	var plans []UpdatePlan
	for _, p := range groups {
		if len(p.Old) > 0 {
			release, err := ResolveRelease(releases, p.Minor)
			if err != nil {
				return nil, err
			}
			p.Release = release
			for _, r := range runtimes {
				if r.Version == release.Version {
					copy := r
					p.Target = &copy
				}
			}
			for _, r := range p.Old {
				if compareVersion(r.Version, release.Version) > 0 {
					return nil, fmt.Errorf("refusing to downgrade PHP %s to %s", r.Version, release.Version)
				}
			}
			// --force never overrides a lock on the destination.
			if locked[release.Version] {
				p.Force = false
			}
			sort.Slice(p.Old, func(a, b int) bool { return compareVersion(p.Old[a].Version, p.Old[b].Version) > 0 })
		}
		plans = append(plans, *p)
	}
	sort.Slice(plans, func(a, b int) bool { return compareVersion(plans[a].Minor, plans[b].Minor) < 0 })
	return plans, nil
}

func (i Installer) PlanUpdates(releases []Release, minor string, force bool) ([]UpdatePlan, error) {
	runtimes, err := i.Store.PHPRuntimes()
	if err != nil {
		return nil, err
	}
	locked := map[string]bool{}
	for _, r := range runtimes {
		locked[r.Version], err = i.Store.PHPLocked(r.Version)
		if err != nil {
			return nil, err
		}
	}
	plans, err := SelectUpdates(runtimes, locked, releases, minor, force)
	if err != nil {
		return nil, err
	}
	projects, err := i.Store.Projects()
	if err != nil {
		return nil, err
	}
	global, _, err := i.Store.MetaValue("global_php_version")
	if err != nil {
		return nil, err
	}
	for n := range plans {
		p := &plans[n]
		for _, old := range p.Old {
			if old.Version == global {
				p.Global = true
			}
			for _, project := range projects {
				if project.PHPVersion == old.Version {
					p.Projects = append(p.Projects, project)
				}
			}
		}
		if p.Needed() {
			if err := i.prepareUpdate(p); err != nil {
				p.Problem = err.Error()
			}
		}
	}
	return plans, nil
}

func (i Installer) prepareUpdate(p *UpdatePlan) error {
	// Both settings and runtime paths must be unambiguous before stopping hosting.
	candidates := append([]state.PHPRuntime{}, p.Old...)
	if p.Target != nil && !containsVersion(p.Old, p.Target.Version) {
		candidates = append(candidates, *p.Target)
	}
	var config map[string]runtimeConfigFile
	for n, r := range candidates {
		if filepath.Clean(r.Prefix) != filepath.Join(i.Paths.PHPDir, r.Version) {
			return fmt.Errorf("PHP %s has an unmanaged prefix %s", r.Version, r.Prefix)
		}
		info, err := os.Lstat(r.Prefix)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("runtime prefix is not a regular directory: %s", r.Prefix)
		}
		extensions, err := i.extensionConfig(r.Version, true)
		if err != nil {
			return err
		}
		sort.Slice(extensions, func(a, b int) bool { return extensions[a].Name < extensions[b].Name })
		files, err := runtimeConfig(r.Prefix)
		if err != nil {
			return err
		}
		if n == 0 {
			p.Extensions = extensions
			config = files
			continue
		}
		if !reflect.DeepEqual(extensions, p.Extensions) {
			return fmt.Errorf("conflicting extension lists in PHP %s; lock patches that must retain separate settings", p.Minor)
		}
		if !reflect.DeepEqual(files, config) {
			return fmt.Errorf("conflicting PHP configuration in %s; lock patches that must retain separate settings", p.Minor)
		}
	}
	// An existing target manifest section must not silently override inherited settings.
	ext, err := i.extensionConfig(p.Release.Version, true)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(i.ExtensionConfigPath())
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	manifest, _, err := parseExtensions(string(data))
	if err != nil {
		return err
	}
	if _, exists := manifest.PHP[p.Release.Version]; exists {
		sort.Slice(ext, func(a, b int) bool { return ext[a].Name < ext[b].Name })
		if !reflect.DeepEqual(ext, p.Extensions) {
			return fmt.Errorf("target PHP %s has a different extension configuration", p.Release.Version)
		}
	}
	return nil
}

func containsVersion(runtimes []state.PHPRuntime, version string) bool {
	for _, r := range runtimes {
		if r.Version == version {
			return true
		}
	}
	return false
}

// Normalize prefix references to compare configs and relocate them to the new prefix.
type runtimeConfigFile struct {
	Data string
	Mode fs.FileMode
}

func runtimeConfig(prefix string) (map[string]runtimeConfigFile, error) {
	result := map[string]runtimeConfigFile{}
	root := filepath.Join(prefix, "etc")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("cannot migrate non-regular config file %s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		result[relative] = runtimeConfigFile{Data: strings.ReplaceAll(string(data), prefix, "{{HERDLITE_PHP_PREFIX}}"), Mode: info.Mode().Perm()}
		return nil
	})
	return result, err
}

type savedFile struct {
	path   string
	data   []byte
	mode   fs.FileMode
	exists bool
}

func saveFile(path string) (savedFile, error) {
	f := savedFile{path: path, mode: 0644}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	if !info.Mode().IsRegular() {
		return f, fmt.Errorf("not a regular file: %s", path)
	}
	f.exists = true
	f.mode = info.Mode().Perm()
	f.data, err = os.ReadFile(path)
	return f, err
}
func (f savedFile) restore() error {
	if !f.exists {
		err := os.Remove(f.path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.WriteFile(f.path, f.data, f.mode)
}

// UpdateResult keeps rollback data until hosting has successfully resumed.
type UpdateResult struct {
	installer Installer
	Plan      UpdatePlan
	Runtime   state.PHPRuntime
	before    state.PHPUpdateState
	files     []savedFile
	backup    string
	created   bool
	switched  bool
}

func (r *UpdateResult) Rollback() error {
	var errs []error
	if r.switched {
		if err := r.installer.Store.RestorePHPUpdate(r.before, r.Runtime.Version); err != nil {
			return fmt.Errorf("restore registration (runtime directories retained): %w", err)
		}
	}
	for n := len(r.files) - 1; n >= 0; n-- {
		errs = append(errs, r.files[n].restore())
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("restore configuration (runtime directories retained): %w", err)
	}
	if r.created {
		errs = append(errs, os.RemoveAll(r.Runtime.Prefix))
	}
	if r.backup != "" {
		errs = append(errs, os.Rename(r.backup, r.Runtime.Prefix))
	}
	return errors.Join(errs...)
}

func (r *UpdateResult) Cleanup() error {
	var errs []error
	for _, old := range r.Plan.Old {
		if old.Version != r.Runtime.Version {
			errs = append(errs, os.RemoveAll(old.Prefix))
		}
	}
	if r.backup != "" {
		errs = append(errs, os.RemoveAll(r.backup))
	}
	return errors.Join(errs...)
}

// ApplyUpdate leaves old patches on disk until Cleanup, including on interrupted batches.
// A non-nil result on error means rollback failed and the batch must stop.
func (i Installer) ApplyUpdate(ctx context.Context, p UpdatePlan, verbose, keepBuild bool) (*UpdateResult, error) {
	return i.applyUpdate(ctx, p, verbose, keepBuild, i.Install)
}

func (i Installer) applyUpdate(ctx context.Context, p UpdatePlan, verbose, keepBuild bool, install func(context.Context, InstallOptions) (state.PHPRuntime, error)) (result *UpdateResult, err error) {
	if !p.Needed() {
		return nil, fmt.Errorf("no update needed")
	}
	if p.Problem != "" {
		return nil, errors.New(p.Problem)
	}
	// Recheck locks/configuration after confirmation, before any mutation.
	for _, old := range p.Old {
		locked, e := i.Store.PHPLocked(old.Version)
		if e != nil {
			return nil, e
		}
		if locked {
			return nil, fmt.Errorf("PHP %s is now locked", old.Version)
		}
	}
	if err := i.prepareUpdate(&p); err != nil {
		return nil, err
	}
	target := runtimeRecord(p.Release, filepath.Join(i.Paths.PHPDir, p.Release.Version))
	if p.Target != nil && !p.Force {
		target = *p.Target
	}
	r := &UpdateResult{installer: i, Plan: p, Runtime: target, before: state.PHPUpdateState{Old: p.Old, Target: p.Target, Projects: p.Projects}}
	r.before.Global, r.before.HasGlobal, err = i.Store.MetaValue("global_php_version")
	if err != nil {
		return nil, err
	}
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
	manifest, e := saveFile(i.ExtensionConfigPath())
	if e != nil {
		return nil, e
	}
	r.files = append(r.files, manifest)
	config, e := runtimeConfig(p.Old[0].Prefix)
	if e != nil {
		return nil, e
	}
	if p.Target != nil && !p.Force {
		// Existing (possibly locked) targets are reused without changing their files.
		if err = ValidateRuntimeContext(ctx, target); err != nil {
			return nil, err
		}
		for _, extension := range p.Extensions {
			check := exec.CommandContext(ctx, target.PHPBinary, "-r", `exit(extension_loaded($argv[1]) ? 0 : 1);`, extension.Name)
			check.Env = i.extensionEnv(target)
			if output, e := check.CombinedOutput(); e != nil {
				return nil, fmt.Errorf("extension %s did not load in existing PHP %s: %w: %s", extension.Name, target.Version, e, output)
			}
		}
	} else {
		if p.Target == nil {
			if _, e := os.Lstat(target.Prefix); e == nil {
				return nil, fmt.Errorf("unregistered target directory already exists: %s", target.Prefix)
			} else if !os.IsNotExist(e) {
				return nil, e
			}
		} else {
			// The compiled-in prefix must remain the real destination during a forced rebuild.
			backup, e := os.MkdirTemp(i.Paths.PHPDir, ".update-"+target.Version+"-")
			if e != nil {
				return nil, e
			}
			if e = os.Remove(backup); e != nil {
				return nil, e
			}
			if e = os.Rename(target.Prefix, backup); e != nil {
				return nil, e
			}
			r.backup = backup
		}
		r.created = true
		if err = i.writeUpdateExtensions(target.Version, p.Extensions); err != nil {
			return nil, err
		}
		r.Runtime, err = install(ctx, InstallOptions{Requested: target.Version, Release: &p.Release, Force: true, DeferRegister: true, Verbose: verbose, KeepBuild: keepBuild, Configure: func(runtime state.PHPRuntime) error {
			for relative, data := range config {
				path := filepath.Join(runtime.Prefix, "etc", relative)
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					return err
				}
				if err := os.WriteFile(path, []byte(strings.ReplaceAll(data.Data, "{{HERDLITE_PHP_PREFIX}}", runtime.Prefix)), data.Mode); err != nil {
					return err
				}
				if err := os.Chmod(path, data.Mode); err != nil {
					return err
				}
			}
			return nil
		}})
		if err != nil {
			r.Runtime = target
			return nil, err
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	// Keep PHP file sessions across replacements. Locked targets remain untouched.
	targetLocked, e := i.Store.PHPLocked(target.Version)
	if e != nil {
		return nil, e
	}
	if !targetLocked {
		for _, old := range p.Old {
			source := old.Prefix
			if source == target.Prefix {
				if r.backup == "" {
					continue
				}
				source = r.backup
			}
			if err = r.copySessions(source, target.Prefix); err != nil {
				return nil, err
			}
		}
	}
	manager := nginx.Manager{Paths: i.Paths}
	for _, project := range p.Projects {
		path := manager.SiteConfigPath(project)
		file, e := saveFile(path)
		if e != nil {
			return nil, e
		}
		r.files = append(r.files, file)
		if !project.Enabled {
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
	if err = i.Store.ApplyPHPUpdate(r.before, target); err != nil {
		return nil, err
	}
	r.switched = true
	return r, nil
}

func (i Installer) writeUpdateExtensions(version string, extensions []Extension) error {
	// Migrate legacy manifests first, retaining original per-version comments.
	if _, err := i.extensionConfig("", false); err != nil {
		return err
	}
	data, err := os.ReadFile(i.ExtensionConfigPath())
	if err != nil {
		return err
	}
	manifest, _, err := parseExtensions(string(data))
	if err != nil {
		return err
	}
	if _, exists := manifest.PHP[version]; exists {
		return nil
	}
	var section strings.Builder
	if err := toml.NewEncoder(&section).Encode(struct {
		PHP map[string]PHPExtensions `toml:"php"`
	}{map[string]PHPExtensions{version: {Extensions: extensions}}}); err != nil {
		return err
	}
	return os.WriteFile(i.ExtensionConfigPath(), append(data, []byte("\n"+section.String())...), 0644)
}

func (r *UpdateResult) copySessions(source, target string) error {
	root := filepath.Join(source, "var", "sessions")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			return fmt.Errorf("cannot migrate session %s", entry.Name())
		}
		data, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			return err
		}
		destination := filepath.Join(target, "var", "sessions", entry.Name())
		saved, err := saveFile(destination)
		if err != nil {
			return err
		}
		if saved.exists {
			if string(saved.data) != string(data) {
				return fmt.Errorf("session conflict: %s", entry.Name())
			}
			continue
		}
		r.files = append(r.files, saved)
		if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(destination, data, 0600); err != nil {
			return err
		}
	}
	return nil
}
