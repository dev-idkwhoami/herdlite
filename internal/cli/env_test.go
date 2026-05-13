package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdlite/internal/app"
	"herdlite/internal/paths"
	"herdlite/internal/state"
)

func TestEnvApplyDatabaseIncludesAppIdentity(t *testing.T) {
	root := t.TempDir()
	projectPath := testLaravelProject(t, root, "customer-portal")
	p := paths.ResolveForHome(root)
	a := &app.App{
		Out:   &bytes.Buffer{},
		Err:   &bytes.Buffer{},
		Paths: p,
		Store: state.NewStore(p.StateFile),
	}

	project, err := a.Store.AddProjectWithOptions(projectPath, state.ProjectOptions{
		Domain:     "customer-portal.test",
		PHPVersion: "8.4.1",
	})
	if err != nil {
		t.Fatal(err)
	}

	code := runEnvApply(a, []string{"--p", project.Name, "--database"})
	if code != 0 {
		t.Fatalf("runEnvApply exited %d: %s", code, a.Err.(*bytes.Buffer).String())
	}

	data, err := os.ReadFile(filepath.Join(projectPath, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, expected := range []string{
		`APP_NAME="Customer Portal"`,
		"APP_URL=https://customer-portal.test",
		"DB_DATABASE=customer_portal",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("expected %q in:\n%s", expected, content)
		}
	}
	if strings.Contains(content, "MAIL_MAILER=") {
		t.Fatalf("did not expect mail env for --database:\n%s", content)
	}
}

func testLaravelProject(t *testing.T, root string, name string) string {
	t.Helper()
	projectPath := filepath.Join(root, name)
	publicPath := filepath.Join(projectPath, "public")
	if err := os.MkdirAll(publicPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publicPath, "index.php"), []byte("<?php\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return projectPath
}
