package laravel

import (
	"os"
	"path/filepath"
	"strings"
)

func ApplyEnv(projectPath string, values map[string]string) error {
	envPath := filepath.Join(projectPath, ".env")
	data, err := os.ReadFile(envPath)
	if os.IsNotExist(err) {
		data = nil
	} else if err != nil {
		return err
	}

	lines := []string{}
	if len(data) > 0 {
		lines = strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
	}

	seen := map[string]bool{}
	for i, line := range lines {
		key, ok := envKey(line)
		if !ok {
			continue
		}
		value, exists := values[key]
		if !exists {
			continue
		}
		lines[i] = key + "=" + value
		seen[key] = true
	}

	for _, key := range sortedEnvKeys(values) {
		if seen[key] {
			continue
		}
		lines = append(lines, key+"="+values[key])
	}

	output := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(envPath, []byte(output), 0o644)
}

func WebsocketEnv(domain string) map[string]string {
	return map[string]string{
		"REVERB_HOST":        domain,
		"REVERB_PORT":        "443",
		"REVERB_SCHEME":      "https",
		"VITE_REVERB_HOST":   `"${REVERB_HOST}"`,
		"VITE_REVERB_PORT":   `"${REVERB_PORT}"`,
		"VITE_REVERB_SCHEME": `"${REVERB_SCHEME}"`,
	}
}

func DatabaseEnv(database string) map[string]string {
	return map[string]string{
		"DB_CONNECTION": "pgsql",
		"DB_HOST":       "127.0.0.1",
		"DB_PORT":       "5432",
		"DB_DATABASE":   database,
		"DB_USERNAME":   "root",
		"DB_PASSWORD":   "",
	}
}

func MailEnv(fromAddress string) map[string]string {
	return map[string]string{
		"MAIL_MAILER":       "smtp",
		"MAIL_HOST":         "127.0.0.1",
		"MAIL_PORT":         "1025",
		"MAIL_USERNAME":     "null",
		"MAIL_PASSWORD":     "null",
		"MAIL_ENCRYPTION":   "null",
		"MAIL_FROM_ADDRESS": fromAddress,
	}
}

func envKey(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	index := strings.Index(trimmed, "=")
	if index <= 0 {
		return "", false
	}
	key := trimmed[:index]
	for _, r := range key {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return "", false
	}
	return key, true
}

func sortedEnvKeys(values map[string]string) []string {
	preferred := []string{
		"DB_CONNECTION",
		"DB_HOST",
		"DB_PORT",
		"DB_DATABASE",
		"DB_USERNAME",
		"DB_PASSWORD",
		"MAIL_MAILER",
		"MAIL_HOST",
		"MAIL_PORT",
		"MAIL_USERNAME",
		"MAIL_PASSWORD",
		"MAIL_ENCRYPTION",
		"MAIL_FROM_ADDRESS",
		"REVERB_HOST",
		"REVERB_PORT",
		"REVERB_SCHEME",
		"VITE_REVERB_HOST",
		"VITE_REVERB_PORT",
		"VITE_REVERB_SCHEME",
	}

	out := []string{}
	added := map[string]bool{}
	for _, key := range preferred {
		if _, ok := values[key]; ok {
			out = append(out, key)
			added[key] = true
		}
	}
	for key := range values {
		if !added[key] {
			out = append(out, key)
		}
	}
	return out
}
