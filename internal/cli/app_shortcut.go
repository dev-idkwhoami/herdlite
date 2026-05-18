package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"herdlite/internal/app"
)

const desktopFileName = "herdlite-debug.desktop"

func runApp(a *app.App, args []string) int {
	if len(args) == 0 || hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite app <install|remove>")
		return codeForUsage(args, 1)
	}

	switch args[0] {
	case "install":
		if len(args) != 1 {
			fmt.Fprintln(a.Err, "Usage: herdlite app install")
			return 1
		}
		path, err := installDesktopApp()
		if err != nil {
			fmt.Fprintf(a.Err, "app install: %v\n", err)
			return 1
		}
		fmt.Fprintf(a.Out, "Installed desktop launcher: %s\n", path)
		return 0
	case "remove":
		if len(args) != 1 {
			fmt.Fprintln(a.Err, "Usage: herdlite app remove")
			return 1
		}
		path, err := removeDesktopApp()
		if err != nil {
			fmt.Fprintf(a.Err, "app remove: %v\n", err)
			return 1
		}
		fmt.Fprintf(a.Out, "Removed desktop launcher: %s\n", path)
		return 0
	default:
		fmt.Fprintf(a.Err, "unknown app command: %s\n", args[0])
		return 1
	}
}

func installDesktopApp() (string, error) {
	path, err := desktopAppPath()
	if err != nil {
		return "", err
	}
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve herdlite executable: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(desktopAppContent(executable)), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func removeDesktopApp() (string, error) {
	path, err := desktopAppPath()
	if err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return path, nil
}

func desktopAppPath() (string, error) {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "applications", desktopFileName), nil
}

func desktopAppContent(executable string) string {
	return strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=Herdlite Debug",
		"Comment=Open the Herdlite debug UI",
		"Exec=" + desktopExecArg(executable) + " debug",
		"Terminal=false",
		"Categories=Development;",
		"",
	}, "\n")
}

func desktopExecArg(value string) string {
	if !strings.ContainsAny(value, " \t\n\"'\\") {
		return value
	}
	escaped := strings.ReplaceAll(value, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
	return "\"" + escaped + "\""
}
