package phpmanager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdlite/internal/paths"
)

func TestRenderConfig(t *testing.T) {
	root := t.TempDir()
	prefix := filepath.Join(root, "php")
	appPaths := paths.ResolveForHome(root)
	paths, err := RenderConfigForPaths(prefix, appPaths)
	if err != nil {
		t.Fatal(err)
	}

	assertFileContains(t, paths.PHPIni, "opcache.enable = 1")
	assertFileContains(t, paths.PHPFPM, "include="+paths.Pool)
	assertFileContains(t, paths.Pool, "listen = "+paths.Socket)
	assertFileContains(t, paths.RuntimeINI, "auto_prepend_file="+paths.PrependFile)
	assertFileContains(t, paths.PrependFile, "VarDumper::setHandler")
	assertFileContains(t, paths.PrependFile, "$_SERVER['VAR_DUMPER_FORMAT'] = 'server';")
}

func assertFileContains(t *testing.T, path string, expected string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), expected) {
		t.Fatalf("expected %s to contain %q, got:\n%s", path, expected, string(data))
	}
}
