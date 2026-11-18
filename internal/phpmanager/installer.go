package phpmanager

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"herdlite/internal/paths"
	"herdlite/internal/state"
)

type Installer struct {
	Paths paths.Paths
	Store *state.Store
	Out   io.Writer
	Err   io.Writer
}

type InstallOptions struct {
	Requested string
	DryRun    bool
	Force     bool
	KeepBuild bool
	Verbose   bool
	// Internal update hooks keep registration separate from building.
	Release       *Release
	DeferRegister bool
	Configure     func(state.PHPRuntime) error
}

func (i Installer) Install(ctx context.Context, opts InstallOptions) (state.PHPRuntime, error) {
	if _, err := i.extensionConfig("", true); err != nil {
		return state.PHPRuntime{}, err
	}
	var release Release
	if opts.Release != nil {
		release = *opts.Release
	} else {
		releases, err := (ReleaseClient{}).Releases(ctx)
		if err != nil {
			return state.PHPRuntime{}, err
		}
		release, err = ResolveRelease(releases, opts.Requested)
		if err != nil {
			return state.PHPRuntime{}, err
		}
	}

	prefix := filepath.Join(i.Paths.PHPDir, release.Version)
	archivePath := filepath.Join(i.Paths.DownloadDir, release.Tag+".tar.gz")
	sourceParent := filepath.Join(i.Paths.SourceDir, release.Tag)
	runtime := runtimeRecord(release, prefix)

	i.printf("Resolved PHP %s to %s\n", opts.RequestedOrLatest(), release.Tag)
	i.printf("Install prefix: %s\n", prefix)
	i.printf("Download: %s\n", release.TarballURL)

	if opts.DryRun {
		if err := i.SyncExtensions(ctx, runtime, true); err != nil {
			return state.PHPRuntime{}, err
		}
		i.printf("Dry run only; PHP was not downloaded or built.\n")
		return runtimeRecord(release, prefix), nil
	}

	if !opts.Force {
		if err := ValidateRuntimeContext(ctx, runtime); err == nil {
			i.printf("PHP %s is already installed and valid; skipping build.\n", runtime.Version)
			if err := i.SyncExtensions(ctx, runtime, false); err != nil {
				return state.PHPRuntime{}, err
			}
			if err := i.register(runtime, opts.DeferRegister); err != nil {
				return state.PHPRuntime{}, err
			}
			return runtime, nil
		} else if runtimeLooksInstalled(runtime) {
			i.printf("Existing PHP %s install is invalid; rebuilding: %v\n", runtime.Version, err)
		}
	}

	if err := os.MkdirAll(i.Paths.DownloadDir, 0o755); err != nil {
		return state.PHPRuntime{}, err
	}
	if err := os.MkdirAll(i.Paths.SourceDir, 0o755); err != nil {
		return state.PHPRuntime{}, err
	}
	if err := os.MkdirAll(i.Paths.PHPDir, 0o755); err != nil {
		return state.PHPRuntime{}, err
	}

	if err := download(ctx, release.TarballURL, archivePath); err != nil {
		return state.PHPRuntime{}, err
	}

	if err := os.RemoveAll(sourceParent); err != nil {
		return state.PHPRuntime{}, err
	}
	if err := os.MkdirAll(sourceParent, 0o755); err != nil {
		return state.PHPRuntime{}, err
	}

	sourceDir, err := ExtractTarGz(archivePath, sourceParent)
	if err != nil {
		return state.PHPRuntime{}, err
	}

	if err := i.build(ctx, sourceDir, prefix, opts.Verbose); err != nil {
		return state.PHPRuntime{}, err
	}
	if _, err := RenderConfigForPaths(prefix, i.Paths); err != nil {
		return state.PHPRuntime{}, fmt.Errorf("render PHP config: %w", err)
	}

	if opts.Configure != nil {
		if err := opts.Configure(runtime); err != nil {
			return state.PHPRuntime{}, err
		}
	}
	if err := i.SyncExtensions(ctx, runtime, false); err != nil {
		return state.PHPRuntime{}, err
	}
	if err := ValidateRuntimeContext(ctx, runtime); err != nil {
		return state.PHPRuntime{}, fmt.Errorf("validate PHP runtime: %w", err)
	}

	if err := i.register(runtime, opts.DeferRegister); err != nil {
		return state.PHPRuntime{}, err
	}

	if !opts.KeepBuild {
		if err := os.RemoveAll(sourceParent); err != nil {
			i.printf("Warning: failed to remove build source %s: %v\n", sourceParent, err)
		}
	}

	return runtime, nil
}

func runtimeLooksInstalled(runtime state.PHPRuntime) bool {
	_, phpErr := os.Stat(runtime.PHPBinary)
	_, fpmErr := os.Stat(runtime.PHPFPMBinary)
	return phpErr == nil || fpmErr == nil
}

func (opts InstallOptions) RequestedOrLatest() string {
	if opts.Requested == "" {
		return "latest"
	}
	return opts.Requested
}

func (i Installer) build(ctx context.Context, sourceDir string, prefix string, verbose bool) error {
	if _, err := os.Stat(filepath.Join(sourceDir, "configure")); os.IsNotExist(err) {
		if err := i.run(ctx, sourceDir, verbose, "./buildconf", "--force"); err != nil {
			return err
		}
	}

	user := os.Getenv("USER")
	if user == "" {
		user = "nobody"
	}

	configureArgs := []string{
		"--prefix=" + prefix,
		"--with-config-file-path=" + filepath.Join(prefix, "etc"),
		"--with-config-file-scan-dir=" + filepath.Join(prefix, "etc", "conf.d"),
		"--enable-fpm",
		"--with-fpm-user=" + user,
		"--with-fpm-group=" + user,
		"--enable-mbstring",
		"--enable-intl",
		"--enable-bcmath",
		"--enable-gd",
		"--enable-pcntl",
		"--enable-sockets",
		"--with-openssl",
		"--with-curl",
		"--with-freetype",
		"--with-jpeg",
		"--with-webp",
		"--with-zlib",
		"--with-zip",
		"--with-pdo-pgsql",
		"--with-pgsql",
		"--with-pdo-sqlite",
		"--with-sqlite3",
	}

	if err := i.run(ctx, sourceDir, verbose, "./configure", configureArgs...); err != nil {
		return err
	}
	if err := i.run(ctx, sourceDir, verbose, "make", "-j"+fmt.Sprint(runtime.NumCPU())); err != nil {
		return err
	}
	if err := i.run(ctx, sourceDir, verbose, "make", "install"); err != nil {
		return err
	}

	return nil
}

func (i Installer) run(ctx context.Context, dir string, verbose bool, name string, args ...string) error {
	logPath := filepath.Join(i.Paths.LogDir, "php-build-"+safeLogName(dir)+"-"+safeLogName(name)+"-"+time.Now().Format("20060102-150405.000000000")+".log")
	i.printf("Running: %s %v\n", name, args)
	if !verbose {
		i.printf("  log: %s\n", logPath)
	}
	start := time.Now()
	cmd := exec.CommandContext(ctx, name, args...)
	configureBuildCancellation(cmd)
	cmd.Dir = dir

	var logFile *os.File
	if verbose {
		cmd.Stdout = i.Out
		cmd.Stderr = i.Err
	} else {
		if err := os.MkdirAll(i.Paths.LogDir, 0o755); err != nil {
			return err
		}
		file, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		logFile = file
		defer logFile.Close()
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	if err := cmd.Run(); err != nil {
		if verbose {
			return fmt.Errorf("%s failed: %w", name, err)
		}
		return fmt.Errorf("%s failed: %w; see %s", name, err, logPath)
	}
	i.printf("Finished in %s\n", time.Since(start).Round(time.Millisecond))
	return nil
}

func safeLogName(name string) string {
	name = filepath.Base(name)
	if name == "." || name == string(filepath.Separator) || name == "" {
		return "command"
	}
	return name
}

func download(ctx context.Context, url string, destination string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "herdlite")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}

	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func runtimeRecord(release Release, prefix string) state.PHPRuntime {
	return state.PHPRuntime{
		Version:      release.Version,
		Minor:        release.Minor,
		Tag:          release.Tag,
		Source:       "php.net",
		SourceURL:    release.TarballURL,
		Prefix:       prefix,
		PHPBinary:    filepath.Join(prefix, "bin", "php"),
		PHPFPMBinary: filepath.Join(prefix, "sbin", "php-fpm"),
		InstalledAt:  time.Now().UTC(),
	}
}

func (i Installer) printf(format string, args ...any) {
	if i.Out == nil {
		return
	}
	fmt.Fprintf(i.Out, format, args...)
}

func (i Installer) register(runtime state.PHPRuntime, deferRegister bool) error {
	if deferRegister {
		return nil
	}
	return i.Store.UpsertPHPRuntime(runtime)
}
