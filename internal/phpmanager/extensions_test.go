package phpmanager

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"herdlite/internal/paths"
	"herdlite/internal/state"
)

const legacyExtensions = `[[extensions]]
name = "grpc"
package = "grpc/grpc-php-ext"
version = "1.84.0"
`

func manifestInstaller(t *testing.T, data string, versions ...string) Installer {
	t.Helper()
	p := paths.ResolveForHome(t.TempDir())
	store := state.NewStore(p.StateFile)
	for _, version := range versions {
		if err := store.UpsertPHPRuntime(state.PHPRuntime{Version: version, Minor: minorVersion(version)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(p.ConfigDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.ConfigDir, "extensions.toml"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return Installer{Paths: p, Store: store}
}

func TestExtensionMigration(t *testing.T) {
	i := manifestInstaller(t, legacyExtensions, "8.5.6", "8.5.11")
	for _, dry := range []bool{true, false} {
		extensions, err := i.extensionConfig("8.5.11", dry)
		if err != nil || len(extensions) != 1 || extensions[0].Name != "grpc" {
			t.Fatalf("%v %v", extensions, err)
		}
		data, _ := os.ReadFile(i.ExtensionConfigPath())
		if dry && string(data) != legacyExtensions {
			t.Fatal("dry run modified file")
		}
	}
	data, _ := os.ReadFile(i.ExtensionConfigPath())
	config, legacy, err := parseExtensions(string(data))
	if err != nil || legacy || len(config.PHP) != 2 || len(config.PHP["8.5.6"].Extensions) != 1 {
		t.Fatalf("%s: %v", data, err)
	}
	backups, _ := filepath.Glob(i.ExtensionConfigPath() + ".legacy-*.bak")
	if len(backups) != 1 {
		t.Fatal(backups)
	}
	original, _ := os.ReadFile(backups[0])
	if string(original) != legacyExtensions {
		t.Fatal("backup differs")
	}
	if _, err := i.extensionConfig("8.5.11", false); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(i.ExtensionConfigPath())
	if !bytes.Equal(data, again) {
		t.Fatal("repeat sync rewrote config")
	}
}

func TestVersionInheritanceAndIsolation(t *testing.T) {
	data := "# keep this comment\n[php.\"8.5.6\"]\n" + strings.ReplaceAll(legacyExtensions, "[[extensions]]", "[[php.\"8.5.6\".extensions]]") + "\n[php.\"8.5.11\"]\nextensions = []\n"
	i := manifestInstaller(t, data, "8.5.6", "8.5.11")
	for _, version := range []string{"8.5.12", "8.6.0"} {
		extensions, err := i.extensionConfig(version, false)
		if err != nil || len(extensions) != 0 {
			t.Fatalf("%s: %v %v", version, extensions, err)
		}
	}
	saved, _ := os.ReadFile(i.ExtensionConfigPath())
	if !strings.HasPrefix(string(saved), data) {
		t.Fatal("existing text changed")
	}
	config, _, err := parseExtensions(string(saved))
	if err != nil || len(config.PHP) != 4 {
		t.Fatalf("%s: %v", saved, err)
	}
	// With only 8.5.6 installed, its entries are copied for another 8.5 patch.
	i = manifestInstaller(t, strings.Split(data, "\n[php.\"8.5.11\"]")[0], "8.5.6")
	extensions, err := i.extensionConfig("8.5.11", false)
	if err != nil || len(extensions) != 1 {
		t.Fatalf("%v %v", extensions, err)
	}
	// Per-runtime pins can differ without duplicate-name errors.
	config.PHP["8.5.11"] = PHPExtensions{Extensions: []Extension{{Name: "grpc", Package: "grpc/grpc-php-ext", Version: "1.83.0"}}}
	var encoded strings.Builder
	if err := toml.NewEncoder(&encoded).Encode(config); err != nil {
		t.Fatal(err)
	}
	if _, _, err := parseExtensions(encoded.String()); err != nil {
		t.Fatal(err)
	}
}

func TestParseExtensionsRejectsInvalidConfig(t *testing.T) {
	for _, data := range []string{"[broken", "extension=[]", legacyExtensions + "typo=true", legacyExtensions + legacyExtensions,
		"[php.\"8.5\"]", legacyExtensions + "[php.\"8.5.6\"]", strings.ReplaceAll(legacyExtensions, "grpc/grpc-php-ext", "--force"), strings.ReplaceAll(legacyExtensions, `version = "1.84.0"`, `version = ""`)} {
		if _, _, err := parseExtensions(data); err == nil {
			t.Errorf("accepted %q", data)
		}
	}
}

func TestExtensionDryRunDoesNotDownload(t *testing.T) {
	var out bytes.Buffer
	i := Installer{Paths: paths.ResolveForHome(t.TempDir()), Out: &out}
	if err := i.SyncExtensions(context.Background(), state.PHPRuntime{Version: "8.6.0"}, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no extensions configured") {
		t.Fatal(out.String())
	}
	if _, err := os.Stat(i.Paths.ConfigDir); !os.IsNotExist(err) {
		t.Fatal("dry run created config")
	}
}

func TestPIERejectsCorruptCachedDownload(t *testing.T) {
	i := Installer{Paths: paths.ResolveForHome(t.TempDir())}
	dir := filepath.Join(i.Paths.DataDir, "pie")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pie-"+pieVersion+".phar"), []byte("corrupted"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := i.ensurePIE(context.Background()); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum failure, got %v", err)
	}
}

func TestExtensionEnvironmentTargetsSelectedRuntime(t *testing.T) {
	t.Setenv("PHPRC", "/wrong/php.ini")
	t.Setenv("PHP_INI_SCAN_DIR", "/wrong/conf.d")
	t.Setenv("COMPOSER", "/wrong/composer.json")
	i := Installer{Paths: paths.ResolveForHome(t.TempDir())}
	env := strings.Join(i.extensionEnv(state.PHPRuntime{Prefix: "/selected/php"}), "\n")
	if strings.Contains(env, "/wrong") || !strings.Contains(env, "PHP_INI_SCAN_DIR=/selected/php/etc/conf.d") {
		t.Fatal(env)
	}
}

// Opt-in smoke test. The caller supplies an isolated, relocated PHP runtime.
func TestPIEIntegration(t *testing.T) {
	prefix := os.Getenv("HERDLITE_TEST_PHP_PREFIX")
	if prefix == "" {
		t.Skip("set HERDLITE_TEST_PHP_PREFIX to an isolated PHP installation")
	}
	i := manifestInstaller(t, legacyExtensions, "8.5.6")
	i.Out, i.Err = os.Stdout, os.Stderr
	php := state.PHPRuntime{Prefix: prefix, PHPBinary: filepath.Join(prefix, "bin/php"), Version: "8.5.6"}
	if err := i.SyncExtensions(context.Background(), php, false); err != nil {
		t.Fatal(err)
	}
	// PIE should recognize the same version on the second sync.
	if err := i.SyncExtensions(context.Background(), php, false); err != nil {
		t.Fatal(err)
	}
}
