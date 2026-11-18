package composer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"herdlite/internal/paths"
	"herdlite/internal/state"
)

const pharURL = "https://getcomposer.org/download/latest-stable/composer.phar"

type Manager struct {
	Paths paths.Paths
	Out   io.Writer
}

func (m Manager) Path() string {
	return filepath.Join(m.Paths.ComposerDir, "composer.phar")
}

func (m Manager) Install(ctx context.Context, runtime state.PHPRuntime, dryRun bool) error {
	destination := m.Path()
	m.printf("Install Composer: %s -> %s\n", pharURL, destination)
	if dryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(destination), ".composer-update-*")
	if err != nil {
		return err
	}
	staged := temp.Name()
	if err := temp.Close(); err != nil {
		os.Remove(staged)
		return err
	}
	defer os.Remove(staged)
	if err := download(ctx, pharURL, staged); err != nil {
		return err
	}
	if err := os.Chmod(staged, 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, runtime.PHPBinary, staged, "--version")
	cmd.Stdout = m.Out
	cmd.Stderr = m.Out
	if err := cmd.Run(); err != nil {
		return err
	}
	return os.Rename(staged, destination)
}

func (m Manager) Exists() bool {
	_, err := os.Stat(m.Path())
	return err == nil
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

	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

func (m Manager) printf(format string, args ...any) {
	if m.Out == nil {
		return
	}
	fmt.Fprintf(m.Out, format, args...)
}
