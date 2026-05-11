package phpmanager

import (
	_ "embed"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	herdpaths "herdlite/internal/paths"
)

//go:embed templates/php.ini.tmpl
var phpIniTemplate string

//go:embed templates/php-fpm.conf.tmpl
var phpFPMTemplate string

//go:embed templates/pool.conf.tmpl
var poolTemplate string

//go:embed templates/herdlite-prepend.php.tmpl
var herdlitePrependTemplate string

type ConfigPaths struct {
	PHPIni      string
	PHPFPM      string
	Pool        string
	Socket      string
	PID         string
	ErrorLog    string
	PHPErrorLog string
	SessionDir  string
	ScanDir     string
	PrependFile string
	RuntimeINI  string
}

func RenderConfig(prefix string) (ConfigPaths, error) {
	appPaths, err := herdpaths.Resolve()
	if err != nil {
		return ConfigPaths{}, err
	}
	return RenderConfigForPaths(prefix, appPaths)
}

func RenderConfigForPaths(prefix string, appPaths herdpaths.Paths) (ConfigPaths, error) {
	currentUser := "nobody"
	currentGroup := "nobody"
	if u, err := user.Current(); err == nil {
		currentUser = u.Username
		if group, err := user.LookupGroupId(u.Gid); err == nil {
			currentGroup = group.Name
		} else {
			currentGroup = u.Username
		}
	}

	paths := ConfigPathsForPrefixWithRuntime(prefix, appPaths.PHPRuntimeDir)

	dirs := []string{
		filepath.Join(prefix, "etc"),
		filepath.Join(prefix, "etc", "conf.d"),
		filepath.Join(prefix, "etc", "php-fpm.d"),
		filepath.Join(prefix, "var", "run"),
		filepath.Join(prefix, "var", "log"),
		paths.SessionDir,
		filepath.Dir(paths.PrependFile),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return ConfigPaths{}, err
		}
	}

	if err := os.WriteFile(paths.PHPIni, []byte(phpIniTemplate), 0o644); err != nil {
		return ConfigPaths{}, err
	}
	if err := os.WriteFile(paths.PrependFile, []byte(herdlitePrependTemplate), 0o644); err != nil {
		return ConfigPaths{}, err
	}
	runtimeINI := "auto_prepend_file=" + paths.PrependFile + "\n"
	if err := os.WriteFile(paths.RuntimeINI, []byte(runtimeINI), 0o644); err != nil {
		return ConfigPaths{}, err
	}

	fpm := replace(phpFPMTemplate, map[string]string{
		"{{PID_PATH}}":  paths.PID,
		"{{ERROR_LOG}}": paths.ErrorLog,
		"{{POOL_CONF}}": paths.Pool,
	})
	if err := os.WriteFile(paths.PHPFPM, []byte(fpm), 0o644); err != nil {
		return ConfigPaths{}, err
	}

	pool := replace(poolTemplate, map[string]string{
		"{{POOL_NAME}}":       "herdlite",
		"{{USER}}":            currentUser,
		"{{GROUP}}":           currentGroup,
		"{{SOCKET_PATH}}":     paths.Socket,
		"{{PHP_ERROR_LOG}}":   paths.PHPErrorLog,
		"{{SESSION_DIR}}":     paths.SessionDir,
		"{{BEFORE_SNIPPETS}}": "",
		"{{AFTER_SNIPPETS}}":  "",
	})
	if err := os.WriteFile(paths.Pool, []byte(pool), 0o644); err != nil {
		return ConfigPaths{}, err
	}

	return paths, nil
}

func ConfigPathsForPrefix(prefix string) ConfigPaths {
	appPaths, _ := herdpaths.Resolve()
	return ConfigPathsForPrefixWithRuntime(prefix, appPaths.PHPRuntimeDir)
}

func ConfigPathsForPrefixWithRuntime(prefix string, runtimeDir string) ConfigPaths {
	prependFile := filepath.Join(runtimeDir, "prepend.php")
	return ConfigPaths{
		PHPIni:      filepath.Join(prefix, "etc", "php.ini"),
		PHPFPM:      filepath.Join(prefix, "etc", "php-fpm.conf"),
		Pool:        filepath.Join(prefix, "etc", "php-fpm.d", "herdlite.conf"),
		Socket:      filepath.Join(prefix, "var", "run", "php-fpm.sock"),
		PID:         filepath.Join(prefix, "var", "run", "php-fpm.pid"),
		ErrorLog:    filepath.Join(prefix, "var", "log", "php-fpm.log"),
		PHPErrorLog: filepath.Join(prefix, "var", "log", "php-error.log"),
		SessionDir:  filepath.Join(prefix, "var", "sessions"),
		ScanDir:     filepath.Join(prefix, "etc", "conf.d"),
		PrependFile: prependFile,
		RuntimeINI:  filepath.Join(prefix, "etc", "conf.d", "50-herdlite-runtime.ini"),
	}
}

func replace(template string, replacements map[string]string) string {
	out := template
	for key, value := range replacements {
		out = strings.ReplaceAll(out, key, value)
	}
	return out
}
