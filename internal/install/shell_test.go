package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
