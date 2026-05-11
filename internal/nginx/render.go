package nginx

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"herdlite/internal/certs"
	"herdlite/internal/paths"
	"herdlite/internal/state"
)

//go:embed templates/websocket.conf.tmpl
var websocketTemplate string

//go:embed templates/site.conf.tmpl
var siteTemplate string

//go:embed templates/nginx.conf.tmpl
var baseTemplate string

type Manager struct {
	Paths paths.Paths
}

func (m Manager) WriteBaseConfig() (string, error) {
	mimeTypes := "/etc/nginx/mime.types"
	if _, err := os.Stat(mimeTypes); err != nil {
		if found, lookErr := findMimeTypes(); lookErr == nil {
			mimeTypes = found
		}
	}
	tempDir := filepath.Join(m.Paths.NginxDir, "temp")

	replacements := map[string]string{
		"{{PID_PATH}}":   filepath.Join(m.Paths.NginxDir, "nginx.pid"),
		"{{ERROR_LOG}}":  filepath.Join(m.Paths.LogDir, "nginx-error.log"),
		"{{ACCESS_LOG}}": filepath.Join(m.Paths.LogDir, "nginx-access.log"),
		"{{MIME_TYPES}}": mimeTypes,
		"{{SITES_DIR}}":  m.Paths.NginxSitesDir,
		"{{TEMP_DIR}}":   tempDir,
	}

	content := replaceAll(baseTemplate, replacements)
	if err := os.MkdirAll(m.Paths.NginxSitesDir, 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(m.Paths.LogDir, 0o755); err != nil {
		return "", err
	}
	for _, name := range []string{"client-body", "proxy", "fastcgi", "uwsgi", "scgi"} {
		if err := os.MkdirAll(filepath.Join(tempDir, name), 0o755); err != nil {
			return "", err
		}
	}

	path := filepath.Join(m.Paths.NginxDir, "nginx.conf")
	return path, os.WriteFile(path, []byte(content), 0o644)
}

func (m Manager) WriteSite(project state.Project, runtime state.PHPRuntime, cert certs.SiteCert) (string, error) {
	if project.Domain == "" {
		return "", fmt.Errorf("project domain is empty")
	}
	if project.Path == "" {
		return "", fmt.Errorf("project path is empty")
	}
	if runtime.Prefix == "" {
		return "", fmt.Errorf("PHP runtime prefix is empty")
	}
	fastCGIParams, err := findNginxConfigFile("fastcgi_params")
	if err != nil {
		return "", err
	}

	replacements := map[string]string{
		"{{DOMAIN}}":         project.Domain,
		"{{PUBLIC_ROOT}}":    filepath.Join(project.Path, "public"),
		"{{CERT_PATH}}":      cert.CertPath,
		"{{KEY_PATH}}":       cert.KeyPath,
		"{{PHP_FPM_SOCKET}}": filepath.Join(runtime.Prefix, "var", "run", "php-fpm.sock"),
		"{{FASTCGI_PARAMS}}": fastCGIParams,
	}

	content := replaceAll(siteTemplate, replacements)
	if err := os.MkdirAll(m.Paths.NginxSitesDir, 0o755); err != nil {
		return "", err
	}

	path := m.SiteConfigPath(project)
	return path, os.WriteFile(path, []byte(content), 0o644)
}

func (m Manager) WriteWebsocket(project state.Project, cert certs.SiteCert) (string, error) {
	if !project.Websocket.Enabled {
		return "", m.RemoveWebsocket(project)
	}
	if project.Websocket.Domain == "" {
		return "", fmt.Errorf("websocket domain is empty")
	}
	if project.Websocket.Port == 0 {
		project.Websocket.Port = 8080
	}

	replacements := map[string]string{
		"{{WEBSOCKET_DOMAIN}}":    project.Websocket.Domain,
		"{{WEBSOCKET_CERT_PATH}}": cert.CertPath,
		"{{WEBSOCKET_KEY_PATH}}":  cert.KeyPath,
		"{{WEBSOCKET_PORT}}":      strconv.Itoa(project.Websocket.Port),
	}

	content := replaceAll(websocketTemplate, replacements)
	if err := os.MkdirAll(m.Paths.NginxSitesDir, 0o755); err != nil {
		return "", err
	}

	path := m.WebsocketConfigPath(project)
	return path, os.WriteFile(path, []byte(content), 0o644)
}

func (m Manager) RemoveWebsocket(project state.Project) error {
	err := os.Remove(m.WebsocketConfigPath(project))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (m Manager) WebsocketConfigPath(project state.Project) string {
	return filepath.Join(m.Paths.NginxSitesDir, project.Domain+".ws.conf")
}

func (m Manager) SiteConfigPath(project state.Project) string {
	return filepath.Join(m.Paths.NginxSitesDir, project.Domain+".conf")
}

func replaceAll(template string, replacements map[string]string) string {
	out := template
	for key, value := range replacements {
		out = strings.ReplaceAll(out, key, value)
	}
	return out
}

func findMimeTypes() (string, error) {
	return findNginxConfigFile("mime.types")
}

func findNginxConfigFile(name string) (string, error) {
	direct := filepath.Join("/etc/nginx", name)
	if _, err := os.Stat(direct); err == nil {
		return direct, nil
	}
	nginx, err := exec.LookPath("nginx")
	if err != nil {
		return "", err
	}
	possible := []string{
		filepath.Join(filepath.Dir(filepath.Dir(nginx)), "etc", "nginx", name),
		filepath.Join("/usr/local/etc/nginx", name),
	}
	for _, path := range possible {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s not found", name)
}
