package laravel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppEnvFromDomain(t *testing.T) {
	tests := []struct {
		domain string
		name   string
		url    string
	}{
		{domain: "customer-portal.test", name: `"Customer Portal"`, url: "https://customer-portal.test"},
		{domain: "api.customer.test", name: "Api", url: "https://api.customer.test"},
		{domain: "---.test", name: "App", url: "https://---.test"},
	}

	for _, test := range tests {
		got := AppEnv(test.domain)
		if got["APP_NAME"] != test.name {
			t.Fatalf("APP_NAME for %q = %q, want %q", test.domain, got["APP_NAME"], test.name)
		}
		if got["APP_URL"] != test.url {
			t.Fatalf("APP_URL for %q = %q, want %q", test.domain, got["APP_URL"], test.url)
		}
	}
}

func TestApplyEnvReplacesExistingKeysAndAppendsMissing(t *testing.T) {
	projectPath := t.TempDir()
	envPath := filepath.Join(projectPath, ".env")
	input := strings.Join([]string{
		"APP_NAME=Old App",
		"# DB_DATABASE=commented",
		"DB_DATABASE=old_database",
		"UNCHANGED=value",
		"MAIL_FROM_ADDRESS=old@example.test",
		"",
	}, "\n")
	if err := os.WriteFile(envPath, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}

	values := AppEnv("customer-portal.test")
	for key, value := range DatabaseEnv("customer_portal") {
		values[key] = value
	}
	for key, value := range MailEnv("noreply@customer-portal.test") {
		values[key] = value
	}

	if err := ApplyEnv(projectPath, values); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	assertContains(t, content, `APP_NAME="Customer Portal"`)
	assertContains(t, content, "APP_URL=https://customer-portal.test")
	assertContains(t, content, "DB_DATABASE=customer_portal")
	assertContains(t, content, "MAIL_FROM_ADDRESS=noreply@customer-portal.test")
	assertContains(t, content, "# DB_DATABASE=commented")
	assertContains(t, content, "UNCHANGED=value")

	if strings.Count(content, "APP_NAME=") != 1 {
		t.Fatalf("expected APP_NAME once, got:\n%s", content)
	}
	if strings.Count(content, "DB_DATABASE=") != 2 {
		t.Fatalf("expected one active DB_DATABASE plus one comment, got:\n%s", content)
	}
	if strings.Count(content, "MAIL_FROM_ADDRESS=") != 1 {
		t.Fatalf("expected MAIL_FROM_ADDRESS once, got:\n%s", content)
	}
}

func assertContains(t *testing.T, content string, expected string) {
	t.Helper()
	if !strings.Contains(content, expected) {
		t.Fatalf("expected %q in:\n%s", expected, content)
	}
}
