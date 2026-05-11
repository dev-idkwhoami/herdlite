package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ShellIntegration struct {
	Path      string
	ZshrcPath string
	Appended  bool
}

func WriteZshIntegration(target TargetUser, herdliteBinary string) (ShellIntegration, error) {
	if herdliteBinary == "" {
		return ShellIntegration{}, fmt.Errorf("herdlite binary path is empty")
	}

	shellDir := filepath.Join(target.Paths.ConfigDir, "shell")
	path := filepath.Join(shellDir, "herdlite.zsh")
	content := fmt.Sprintf(`# Herdlite shell integration.
# Source this file from ~/.zshrc to enable project-aware command routing.

_herdlite_bin=%q

case ":$PATH:" in
  *":$HOME/.local/share/herdlite/bin:"*) ;;
  *) export PATH="$HOME/.local/share/herdlite/bin:$PATH" ;;
esac

php() {
  "$_herdlite_bin" shim php "$@"
}

composer() {
  "$_herdlite_bin" shim composer "$@"
}

node() {
  "$_herdlite_bin" shim node "$@"
}

npm() {
  "$_herdlite_bin" shim npm "$@"
}

npx() {
  "$_herdlite_bin" shim npx "$@"
}
`, herdliteBinary)

	if err := os.MkdirAll(shellDir, 0o755); err != nil {
		return ShellIntegration{}, err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return ShellIntegration{}, err
	}
	if os.Geteuid() == 0 {
		if err := os.Chown(shellDir, target.UID, target.GID); err != nil {
			return ShellIntegration{}, err
		}
		if err := os.Chown(path, target.UID, target.GID); err != nil {
			return ShellIntegration{}, err
		}
	}

	zshrcPath, appended, err := ensureZshrcHook(target, path)
	if err != nil {
		return ShellIntegration{}, err
	}

	return ShellIntegration{
		Path:      path,
		ZshrcPath: zshrcPath,
		Appended:  appended,
	}, nil
}

func DisableZshIntegration(target TargetUser, dryRun bool) (string, error) {
	path := filepath.Join(target.Paths.ConfigDir, "shell", "herdlite.zsh")
	zshrcPath := filepath.Join(target.HomeDir, ".zshrc")
	if dryRun {
		return path, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return path, nil
	}
	if err != nil {
		return path, err
	}

	lines := strings.Split(string(data), "\n")
	out := []string{"# Disabled by Herdlite uninstall."}
	for _, line := range lines {
		if line == "" {
			out = append(out, "#")
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			out = append(out, line)
			continue
		}
		out = append(out, "# "+line)
	}
	if err := os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644); err != nil {
		return path, err
	}
	if err := commentZshrcHook(zshrcPath); err != nil {
		return path, err
	}
	return path, nil
}

func ensureZshrcHook(target TargetUser, integrationPath string) (string, bool, error) {
	zshrcPath := filepath.Join(target.HomeDir, ".zshrc")
	sourceLine := fmt.Sprintf("source %q", integrationPath)
	block := "\n# >>> Herdlite shell integration >>>\n" + sourceLine + "\n# <<< Herdlite shell integration <<<\n"

	data, err := os.ReadFile(zshrcPath)
	if os.IsNotExist(err) {
		data = nil
	} else if err != nil {
		return zshrcPath, false, err
	}
	if strings.Contains(string(data), sourceLine) {
		return zshrcPath, false, nil
	}

	file, err := os.OpenFile(zshrcPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return zshrcPath, false, err
	}
	if _, err := file.WriteString(block); err != nil {
		file.Close()
		return zshrcPath, false, err
	}
	if err := file.Close(); err != nil {
		return zshrcPath, false, err
	}
	if os.Geteuid() == 0 {
		if err := os.Chown(zshrcPath, target.UID, target.GID); err != nil {
			return zshrcPath, false, err
		}
	}
	return zshrcPath, true, nil
}

func commentZshrcHook(zshrcPath string) error {
	data, err := os.ReadFile(zshrcPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	inBlock := false
	changed := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(line, ">>> Herdlite shell integration >>>") || strings.Contains(line, ">>> herdlite bootstrap >>>") {
			inBlock = true
		}
		if inBlock && line != "" && !strings.HasPrefix(trimmed, "#") {
			lines[i] = "# " + line
			changed = true
		} else if isHerdliteShellLine(trimmed) {
			lines[i] = "# " + line
			changed = true
		}
		if strings.Contains(line, "<<< Herdlite shell integration <<<") || strings.Contains(line, "<<< herdlite bootstrap <<<") {
			inBlock = false
		}
	}
	if !changed {
		return nil
	}
	return os.WriteFile(zshrcPath, []byte(strings.Join(lines, "\n")), 0o644)
}

func isHerdliteShellLine(trimmed string) bool {
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}
	return trimmed == `export PATH="$HOME/.local/share/herdlite/bin:$PATH"` ||
		trimmed == `export PATH="$HOME/.local/share/herdlite/bin:${PATH}"` ||
		strings.HasPrefix(trimmed, `source "$HOME/.config/herdlite/shell/herdlite.zsh"`) ||
		strings.HasPrefix(trimmed, `source "$HOME/.config/herdlite/shell/zsh.zsh"`)
}
