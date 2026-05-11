package paths

import (
	"os"
	"path/filepath"
)

type Paths struct {
	ConfigDir string
	DataDir   string
	CacheDir  string

	StateFile     string
	LogDir        string
	RuntimeDir    string
	NginxDir      string
	PHPDir        string
	ComposerDir   string
	PostgresDir   string
	MailDir       string
	PHPRuntimeDir string
	CertsDir      string
	CADir         string
	SiteCertDir   string
	NginxSitesDir string
	BuildDir      string
	SourceDir     string
	DownloadDir   string
}

func Resolve() (Paths, error) {
	configBase, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, err
	}

	cacheBase, err := os.UserCacheDir()
	if err != nil {
		return Paths{}, err
	}

	dataBase := os.Getenv("XDG_DATA_HOME")
	if dataBase == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
		dataBase = filepath.Join(home, ".local", "share")
	}

	configDir := filepath.Join(configBase, "herdlite")
	dataDir := filepath.Join(dataBase, "herdlite")
	cacheDir := filepath.Join(cacheBase, "herdlite")

	return Paths{
		ConfigDir:     configDir,
		DataDir:       dataDir,
		CacheDir:      cacheDir,
		StateFile:     filepath.Join(configDir, "herdlite.db"),
		LogDir:        filepath.Join(dataDir, "logs"),
		RuntimeDir:    filepath.Join(dataDir, "runtime"),
		NginxDir:      filepath.Join(dataDir, "nginx"),
		NginxSitesDir: filepath.Join(dataDir, "nginx", "sites"),
		PHPDir:        filepath.Join(dataDir, "php"),
		ComposerDir:   filepath.Join(dataDir, "composer"),
		PostgresDir:   filepath.Join(dataDir, "postgres"),
		MailDir:       filepath.Join(dataDir, "mail"),
		PHPRuntimeDir: filepath.Join(dataDir, "php-runtime"),
		CertsDir:      filepath.Join(dataDir, "certs"),
		CADir:         filepath.Join(dataDir, "certs", "ca"),
		SiteCertDir:   filepath.Join(dataDir, "certs", "sites"),
		BuildDir:      filepath.Join(cacheDir, "builds"),
		SourceDir:     filepath.Join(cacheDir, "builds", "php-src"),
		DownloadDir:   filepath.Join(cacheDir, "downloads"),
	}, nil
}

func ResolveForHome(home string) Paths {
	configDir := filepath.Join(home, ".config", "herdlite")
	dataDir := filepath.Join(home, ".local", "share", "herdlite")
	cacheDir := filepath.Join(home, ".cache", "herdlite")

	return Paths{
		ConfigDir:     configDir,
		DataDir:       dataDir,
		CacheDir:      cacheDir,
		StateFile:     filepath.Join(configDir, "herdlite.db"),
		LogDir:        filepath.Join(dataDir, "logs"),
		RuntimeDir:    filepath.Join(dataDir, "runtime"),
		NginxDir:      filepath.Join(dataDir, "nginx"),
		NginxSitesDir: filepath.Join(dataDir, "nginx", "sites"),
		PHPDir:        filepath.Join(dataDir, "php"),
		ComposerDir:   filepath.Join(dataDir, "composer"),
		PostgresDir:   filepath.Join(dataDir, "postgres"),
		MailDir:       filepath.Join(dataDir, "mail"),
		PHPRuntimeDir: filepath.Join(dataDir, "php-runtime"),
		CertsDir:      filepath.Join(dataDir, "certs"),
		CADir:         filepath.Join(dataDir, "certs", "ca"),
		SiteCertDir:   filepath.Join(dataDir, "certs", "sites"),
		BuildDir:      filepath.Join(cacheDir, "builds"),
		SourceDir:     filepath.Join(cacheDir, "builds", "php-src"),
		DownloadDir:   filepath.Join(cacheDir, "downloads"),
	}
}

func (p Paths) EnsureUserDirs() error {
	dirs := []string{
		p.ConfigDir,
		p.DataDir,
		p.CacheDir,
		p.LogDir,
		p.RuntimeDir,
		p.NginxDir,
		p.NginxSitesDir,
		p.PHPDir,
		p.ComposerDir,
		p.PostgresDir,
		p.MailDir,
		p.PHPRuntimeDir,
		p.CertsDir,
		p.CADir,
		p.SiteCertDir,
		p.BuildDir,
		p.SourceDir,
		p.DownloadDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	return nil
}
