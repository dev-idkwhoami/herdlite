package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdlite/internal/paths"
)

func TestCommentZshrcHookDisablesBootstrapAndShellBlocks(t *testing.T) {
	zshrc := filepath.Join(t.TempDir(), ".zshrc")
	content := `export KEEP=1
# >>> herdlite bootstrap >>>
export PATH="$HOME/.local/share/herdlite/bin:$PATH"
# <<< herdlite bootstrap <<<
# >>> Herdlite shell integration >>>
source "$HOME/.config/herdlite/shell/herdlite.zsh"
# <<< Herdlite shell integration <<<
`
	if err := os.WriteFile(zshrc, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := commentZshrcHook(zshrc); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(zshrc)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if strings.Contains(out, "\nexport PATH=") {
		t.Fatalf("expected bootstrap PATH export to be commented, got:\n%s", out)
	}
	if strings.Contains(out, "\nsource ") {
		t.Fatalf("expected shell source line to be commented, got:\n%s", out)
	}
	if !strings.Contains(out, "export KEEP=1") {
		t.Fatalf("unrelated lines should not be changed, got:\n%s", out)
	}
}

func TestCommentZshrcHookDisablesStandaloneHerdliteLines(t *testing.T) {
	zshrc := filepath.Join(t.TempDir(), ".zshrc")
	content := `export KEEP=1
export PATH="$HOME/.local/share/herdlite/bin:$PATH"
source "$HOME/.config/herdlite/shell/herdlite.zsh"
source "$HOME/.config/herdlite/shell/zsh.zsh"
`
	if err := os.WriteFile(zshrc, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := commentZshrcHook(zshrc); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(zshrc)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, unexpected := range []string{
		"\nexport PATH=",
		"\nsource \"$HOME/.config/herdlite/shell/herdlite.zsh\"",
		"\nsource \"$HOME/.config/herdlite/shell/zsh.zsh\"",
	} {
		if strings.Contains(out, unexpected) {
			t.Fatalf("expected standalone Herdlite line %q to be commented, got:\n%s", unexpected, out)
		}
	}
	if !strings.Contains(out, "export KEEP=1") {
		t.Fatalf("unrelated lines should not be changed, got:\n%s", out)
	}
}

func TestWriteShellShimsWritesExecutableWrappers(t *testing.T) {
	home := t.TempDir()
	target := testTargetUser(home)

	written, err := WriteShellShims(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != len(shellShimNames) {
		t.Fatalf("expected %d shims, got %d", len(shellShimNames), len(written))
	}

	for _, name := range shellShimNames {
		path := filepath.Join(target.Paths.ShimsDir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("expected shim %s: %v", name, err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Fatalf("expected shim %s mode 0755, got %o", name, info.Mode().Perm())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		want := "#!/bin/sh\nexec \"$HOME/.local/share/herdlite/bin/herdlite\" shim " + name + " \"$@\"\n"
		if string(data) != want {
			t.Fatalf("unexpected shim %s content:\n%s", name, data)
		}
	}
}

func TestWriteZshIntegrationUsesPathArrayAndNoFunctions(t *testing.T) {
	home := t.TempDir()
	target := testTargetUser(home)

	integration, err := WriteZshIntegration(target, filepath.Join(target.Paths.BinDir, "herdlite"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(integration.Path)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, expected := range []string{
		"typeset -U path PATH",
		`"$HOME/.local/share/herdlite/shims"`,
		`"$HOME/.local/share/herdlite/bin"`,
		`"$HOME/.config/composer/vendor/bin"`,
		"export PATH",
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("expected zsh integration to contain %q, got:\n%s", expected, out)
		}
	}
	for _, unexpected := range []string{"php() {", "composer() {", "node() {", "npm() {", "npx() {"} {
		if strings.Contains(out, unexpected) {
			t.Fatalf("expected zsh integration not to contain %q, got:\n%s", unexpected, out)
		}
	}
}

func testTargetUser(home string) TargetUser {
	return TargetUser{
		Username: "test",
		HomeDir:  home,
		UID:      os.Getuid(),
		GID:      os.Getgid(),
		Paths:    paths.ResolveForHome(home),
	}
}
