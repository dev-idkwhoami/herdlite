package phpmanager

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"herdlite/internal/state"
)

const pieVersion = "1.5.0"
const pieSHA256 = "d195dc35d311b6e0d5e2a3bd02893628f9898db3c004563f0c3a333a02010873"

type Extension struct {
	Name    string `toml:"name"`
	Package string `toml:"package"`
	Version string `toml:"version"`
}

var extensionNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)
var extensionPackagePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*/[a-z0-9][a-z0-9_.-]*$`)

type PHPExtensions struct {
	Extensions []Extension `toml:"extensions"`
}

type extensionManifest struct {
	PHP        map[string]PHPExtensions `toml:"php"`
	Extensions []Extension              `toml:"extensions,omitempty"` // Previous shared format.
}

func parseExtensions(data string) (extensionManifest, bool, error) {
	var config extensionManifest
	meta, err := toml.Decode(data, &config)
	if err != nil {
		return config, false, err
	}
	if keys := meta.Undecoded(); len(keys) > 0 {
		return config, false, fmt.Errorf("unknown configuration keys: %v", keys)
	}
	legacy := meta.IsDefined("extensions")
	if legacy && meta.IsDefined("php") {
		return config, false, fmt.Errorf("cannot mix shared extensions with per-PHP sections")
	}
	if config.PHP == nil {
		config.PHP = map[string]PHPExtensions{}
	}
	if err := validateExtensions(config.Extensions); err != nil {
		return config, false, err
	}
	for version, section := range config.PHP {
		if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(version) {
			return config, false, fmt.Errorf("PHP section %q must use an exact version", version)
		}
		if err := validateExtensions(section.Extensions); err != nil {
			return config, false, fmt.Errorf("PHP %s: %w", version, err)
		}
	}
	return config, legacy, nil
}

func validateExtensions(extensions []Extension) error {
	names, packages := map[string]bool{}, map[string]bool{}
	for _, e := range extensions {
		if !extensionNamePattern.MatchString(e.Name) {
			return fmt.Errorf("invalid extension name %q", e.Name)
		}
		if !extensionPackagePattern.MatchString(e.Package) {
			return fmt.Errorf("invalid package %q", e.Package)
		}
		if strings.TrimSpace(e.Version) == "" || strings.ContainsAny(e.Version, "\r\n\x00") {
			return fmt.Errorf("extension %s requires a version or version constraint", e.Name)
		}
		if names[e.Name] || packages[e.Package] {
			return fmt.Errorf("duplicate extension %s or package %s", e.Name, e.Package)
		}
		names[e.Name], packages[e.Package] = true, true
	}
	return nil
}

// extensionConfig migrates the shared format and adds missing runtime sections.
// Existing per-version text is retained verbatim, including comments.
func (i Installer) extensionConfig(target string, dryRun bool) ([]Extension, error) {
	path := i.ExtensionConfigPath()
	data, err := os.ReadFile(path)
	missing := os.IsNotExist(err)
	if err != nil && !missing {
		return nil, err
	}
	config, legacy, err := parseExtensions(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var runtimes []state.PHPRuntime
	if i.Store != nil {
		runtimes, err = i.Store.PHPRuntimes()
		if err != nil {
			return nil, err
		}
	}
	added := map[string]PHPExtensions{}
	if legacy {
		for _, php := range runtimes {
			config.PHP[php.Version] = PHPExtensions{Extensions: append([]Extension{}, config.Extensions...)}
		}
		config.Extensions = nil
	}
	// Inherit from the newest installed runtime in the same minor that has a section.
	add := func(version string) {
		if _, exists := config.PHP[version]; exists {
			return
		}
		newest := ""
		for _, php := range runtimes {
			if php.Version == version || minorVersion(php.Version) != minorVersion(version) {
				continue
			}
			if _, exists := config.PHP[php.Version]; exists && (newest == "" || compareVersion(php.Version, newest) > 0) {
				newest = php.Version
			}
		}
		section := PHPExtensions{Extensions: []Extension{}}
		if newest != "" {
			section.Extensions = append(section.Extensions, config.PHP[newest].Extensions...)
		}
		config.PHP[version], added[version] = section, section
	}
	// Deterministic order when an existing installation has no section yet.
	sort.Slice(runtimes, func(a, b int) bool { return compareVersion(runtimes[a].Version, runtimes[b].Version) < 0 })
	for _, php := range runtimes {
		add(php.Version)
	}
	if target != "" {
		add(target)
	}
	if !dryRun && (missing || legacy || len(added) > 0) {
		if err := os.MkdirAll(i.Paths.ConfigDir, 0755); err != nil {
			return nil, err
		}
		var encoded strings.Builder
		if legacy {
			// Preserve the exact old file before converting it.
			backup, err := os.CreateTemp(i.Paths.ConfigDir, "extensions.toml.legacy-*.bak")
			if err != nil {
				return nil, err
			}
			_, writeErr := backup.Write(data)
			closeErr := backup.Close()
			if writeErr != nil {
				return nil, writeErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
			i.printf("Backed up shared extension configuration: %s\n", backup.Name())
			added = config.PHP
		} else {
			encoded.Write(data)
		}
		if missing || legacy {
			encoded.WriteString("# Extensions are configured independently for each exact PHP version.\n")
		}
		if len(added) > 0 {
			encoded.WriteString("\n")
			// Encode only new sections so existing comments and choices stay untouched.
			var sections strings.Builder
			if err := toml.NewEncoder(&sections).Encode(struct {
				PHP map[string]PHPExtensions `toml:"php"`
			}{added}); err != nil {
				return nil, err
			}
			// Avoid redeclaring an existing parent table when appending sections.
			encoded.WriteString(strings.TrimPrefix(sections.String(), "[php]\n"))
		}
		text := encoded.String()
		if _, _, err := parseExtensions(text); err != nil {
			return nil, err
		}
		if err := writeExtensionManifest(path, []byte(text)); err != nil {
			return nil, err
		}
	}
	return config.PHP[target].Extensions, nil
}

func writeExtensionManifest(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".extensions-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	mode := os.FileMode(0644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func (i Installer) ExtensionConfigPath() string {
	return filepath.Join(i.Paths.ConfigDir, "extensions.toml")
}

// Opening an invalid manifest remains possible so the user can repair it.
func (i Installer) EnsureExtensionConfig() (string, error) {
	path := i.ExtensionConfigPath()
	if data, err := os.ReadFile(path); err == nil {
		if _, _, err := parseExtensions(string(data)); err != nil {
			return path, nil
		}
	}
	_, err := i.extensionConfig("", false)
	return path, err
}

// LoadedExtensions reports actual modules, including bundled and manually installed ones.
func (i Installer) LoadedExtensions(ctx context.Context, php state.PHPRuntime) ([]string, error) {
	cmd := exec.CommandContext(ctx, php.PHPBinary, "-r", "echo json_encode(get_loaded_extensions());")
	cmd.Env = i.extensionEnv(php)
	cmd.Stderr = i.Err
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var modules []string
	if err := json.Unmarshal(output, &modules); err != nil {
		return nil, fmt.Errorf("read loaded PHP extensions: %w", err)
	}
	sort.Slice(modules, func(a, b int) bool { return strings.ToLower(modules[a]) < strings.ToLower(modules[b]) })
	return modules, nil
}

// SyncExtensions adds or updates configured extensions without rebuilding PHP.
func (i Installer) SyncExtensions(ctx context.Context, php state.PHPRuntime, dryRun bool) error {
	extensions, err := i.extensionConfig(php.Version, dryRun)
	if err != nil {
		return err
	}
	if len(extensions) == 0 {
		i.printf("PHP %s: no extensions configured.\n", php.Version)
	}
	for _, e := range extensions {
		i.printf("PHP %s extension: %s (%s:%s)\n", php.Version, e.Name, e.Package, e.Version)
	}
	if dryRun || len(extensions) == 0 {
		return nil
	}
	for _, tool := range []string{php.PHPBinary, filepath.Join(php.Prefix, "bin", "php-config"), filepath.Join(php.Prefix, "bin", "phpize")} {
		if err := executable(tool); err != nil {
			return err
		}
	}
	pie, err := i.ensurePIE(ctx)
	if err != nil {
		return err
	}
	// Keep PIE's records and caches with Herdlite, independently of project Composer settings.
	workDir := filepath.Join(i.Paths.DataDir, "pie")
	for _, dir := range []string{workDir, filepath.Join(php.Prefix, "etc", "conf.d")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	for _, e := range extensions {
		args := []string{pie, "install", "--no-interaction", "--with-php-config=" + filepath.Join(php.Prefix, "bin", "php-config"), "--with-phpize-path=" + filepath.Join(php.Prefix, "bin", "phpize"), e.Package + ":" + e.Version}
		cmd := exec.CommandContext(ctx, php.PHPBinary, args...)
		configureBuildCancellation(cmd)
		cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = workDir, i.extensionEnv(php), i.Out, i.Err
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("install %s for PHP %s: %w", e.Name, php.Version, err)
		}
		check := exec.CommandContext(ctx, php.PHPBinary, "-r", `exit(extension_loaded($argv[1]) ? 0 : 1);`, e.Name)
		check.Env = i.extensionEnv(php)
		if output, err := check.CombinedOutput(); err != nil {
			return fmt.Errorf("extension %s did not load in PHP %s: %w: %s", e.Name, php.Version, err, output)
		}
	}
	return nil
}

func (i Installer) extensionEnv(php state.PHPRuntime) []string {
	keys := []string{"PHPRC", "PHP_INI_SCAN_DIR", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "COMPOSER", "COMPOSER_HOME"}
	env := []string{}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		omit := false
		for _, excluded := range keys {
			if key == excluded {
				omit = true
			}
		}
		if !omit {
			env = append(env, value)
		}
	}
	return append(env, "PHPRC="+filepath.Join(php.Prefix, "etc", "php.ini"), "PHP_INI_SCAN_DIR="+filepath.Join(php.Prefix, "etc", "conf.d"), "XDG_CONFIG_HOME="+filepath.Join(i.Paths.DataDir, "pie", "config"), "XDG_CACHE_HOME="+filepath.Join(i.Paths.CacheDir, "pie"), "COMPOSER_HOME="+filepath.Join(i.Paths.DataDir, "pie", "composer"))
}

func verifiedPIE(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if fmt.Sprintf("%x", hash.Sum(nil)) != pieSHA256 {
		return fmt.Errorf("PIE %s checksum mismatch", pieVersion)
	}
	return nil
}

func (i Installer) ensurePIE(ctx context.Context) (string, error) {
	dir := filepath.Join(i.Paths.DataDir, "pie")
	path := filepath.Join(dir, "pie-"+pieVersion+".phar")
	if err := verifiedPIE(path); err == nil {
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(dir, ".pie-*.phar")
	if err != nil {
		return "", err
	}
	tmp := file.Name()
	file.Close()
	defer os.Remove(tmp)
	i.printf("Downloading PIE %s\n", pieVersion)
	if err := download(ctx, "https://github.com/php/pie/releases/download/"+pieVersion+"/pie.phar", tmp); err != nil {
		return "", err
	}
	if err := verifiedPIE(tmp); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}
