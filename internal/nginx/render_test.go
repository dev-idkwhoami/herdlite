package nginx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdlite/internal/certs"
	"herdlite/internal/paths"
	"herdlite/internal/state"
)

func TestWriteBaseConfig(t *testing.T) {
	root := t.TempDir()
	manager := Manager{Paths: paths.Paths{
		NginxDir:      filepath.Join(root, "nginx"),
		NginxSitesDir: filepath.Join(root, "nginx", "sites"),
		LogDir:        filepath.Join(root, "logs"),
	}}

	configPath, err := manager.WriteBaseConfig()
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, filepath.Join(root, "nginx", "sites")+"/*.conf") {
		t.Fatalf("expected sites include in config, got:\n%s", content)
	}
	if !strings.Contains(content, "client_body_temp_path "+filepath.Join(root, "nginx", "temp", "client-body")+";") {
		t.Fatalf("expected user-owned nginx temp path in config, got:\n%s", content)
	}
}

func TestWriteSiteConfig(t *testing.T) {
	root := t.TempDir()
	manager := Manager{Paths: paths.Paths{
		NginxDir:      filepath.Join(root, "nginx"),
		NginxSitesDir: filepath.Join(root, "nginx", "sites"),
		LogDir:        filepath.Join(root, "logs"),
	}}
	project := state.Project{
		Name:   "app",
		Path:   filepath.Join(root, "app"),
		Domain: "app.test",
	}
	runtime := state.PHPRuntime{
		Version: "8.5.6",
		Prefix:  filepath.Join(root, "php", "8.5.6"),
	}
	cert := certs.SiteCert{
		Domain:   "app.test",
		CertPath: filepath.Join(root, "certs", "app.test.crt"),
		KeyPath:  filepath.Join(root, "certs", "app.test.key"),
	}

	path, err := manager.WriteSite(project, runtime, cert)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	expectedSocket := filepath.Join(root, "php", "8.5.6", "var", "run", "php-fpm.sock")
	if !strings.Contains(content, "server_name app.test;") {
		t.Fatalf("expected domain in config, got:\n%s", content)
	}
	if !strings.Contains(content, "root "+filepath.Join(root, "app", "public")+";") {
		t.Fatalf("expected public root in config, got:\n%s", content)
	}
	if !strings.Contains(content, "fastcgi_pass unix:"+expectedSocket+";") {
		t.Fatalf("expected PHP-FPM socket in config, got:\n%s", content)
	}
	if !strings.Contains(content, "include /etc/nginx/fastcgi_params;") {
		t.Fatalf("expected absolute fastcgi_params include in config, got:\n%s", content)
	}
	if !strings.Contains(content, "location ~ ^/livewire-[A-Za-z0-9_-]+/") {
		t.Fatalf("expected Livewire route in config, got:\n%s", content)
	}
}
