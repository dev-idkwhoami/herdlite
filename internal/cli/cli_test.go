package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandRegistryUsesDebugAndAppCommands(t *testing.T) {
	commands := map[string]command{}
	for _, cmd := range allCommands() {
		commands[cmd.Name] = cmd
	}

	for _, name := range []string{"debug", "app"} {
		if _, ok := commands[name]; !ok {
			t.Fatalf("expected command %q to be registered", name)
		}
	}
	for _, name := range []string{"logs", "dumps"} {
		if _, ok := commands[name]; ok {
			t.Fatalf("expected command %q to be removed", name)
		}
	}
}

func TestDesktopAppInstallAndRemove(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)

	path, err := installDesktopApp()
	if err != nil {
		t.Fatal(err)
	}

	wantPath := filepath.Join(dataHome, "applications", desktopFileName)
	if path != wantPath {
		t.Fatalf("expected path %q, got %q", wantPath, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{
		"Type=Application",
		"Name=Herdlite Debug",
		"Exec=",
		" debug",
		"Terminal=false",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected desktop content to contain %q, got:\n%s", want, content)
		}
	}

	removed, err := removeDesktopApp()
	if err != nil {
		t.Fatal(err)
	}
	if removed != path {
		t.Fatalf("expected removed path %q, got %q", path, removed)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected launcher to be removed, got %v", err)
	}

	if _, err := removeDesktopApp(); err != nil {
		t.Fatalf("expected removing absent launcher to succeed: %v", err)
	}
}

func TestDesktopExecArgQuotesSpaces(t *testing.T) {
	got := desktopExecArg(`/tmp/herd lite/herdlite`)
	if got != `"/tmp/herd lite/herdlite"` {
		t.Fatalf("unexpected quoted exec arg: %s", got)
	}
}
