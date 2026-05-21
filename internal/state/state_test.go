package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProjectForWorkingDirectoryFindsLinkedParent(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "herdlite.db"))
	projectPath := laravelProject(t, "app")

	project, err := store.AddProjectWithOptions(projectPath, ProjectOptions{PHPVersion: "8.4.1"})
	if err != nil {
		t.Fatal(err)
	}

	nested := filepath.Join(projectPath, "app", "Http", "Controllers")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	found, ok, err := store.ProjectForWorkingDirectory(nested)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected linked project")
	}
	if found.Name != project.Name {
		t.Fatalf("expected %s, got %s", project.Name, found.Name)
	}
}

func TestSetProjectPHPVersion(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "herdlite.db"))
	projectPath := laravelProject(t, "app")

	project, err := store.AddProjectWithOptions(projectPath, ProjectOptions{PHPVersion: "8.4.1"})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := store.SetProjectPHPVersion(project.Name, "8.5.6")
	if err != nil {
		t.Fatal(err)
	}
	if updated.PHPVersion != "8.5.6" {
		t.Fatalf("expected 8.5.6, got %s", updated.PHPVersion)
	}
}

func TestProjectQueueSettingsDefaultsAndPersistence(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "herdlite.db"))
	projectPath := laravelProject(t, "app")

	project, err := store.AddProjectWithOptions(projectPath, ProjectOptions{PHPVersion: "8.4.1"})
	if err != nil {
		t.Fatal(err)
	}

	defaults, err := store.ProjectQueueSettings(project.Name)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Processes != 1 || defaults.Sleep != 3 || defaults.Tries != 1 || defaults.Memory != 128 {
		t.Fatalf("unexpected defaults: %+v", defaults)
	}

	settings, err := store.SetProjectQueueSettings(project.Name, QueueSettings{
		Connection: " redis ",
		Queue:      "critical,default",
		Processes:  3,
		Backoff:    -1,
		Sleep:      5,
		Timeout:    90,
		Tries:      2,
		Memory:     256,
		Env:        " local ",
		Force:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Connection != "redis" || settings.Env != "local" || settings.Backoff != 0 {
		t.Fatalf("expected normalized settings, got %+v", settings)
	}

	persisted, err := store.ProjectQueueSettings(project.Name)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Connection != "redis" || persisted.Queue != "critical,default" || persisted.Processes != 3 || !persisted.Force {
		t.Fatalf("unexpected persisted settings: %+v", persisted)
	}
}

func TestProjectByNameOrDomain(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "herdlite.db"))
	projectPath := laravelProject(t, "my-app")

	project, err := store.AddProjectWithOptions(projectPath, ProjectOptions{PHPVersion: "8.4.1"})
	if err != nil {
		t.Fatal(err)
	}

	for _, value := range []string{project.Name, project.Domain} {
		found, ok, err := store.ProjectByNameOrDomain(value)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatalf("expected project for %s", value)
		}
		if found.Name != project.Name {
			t.Fatalf("expected %s, got %s", project.Name, found.Name)
		}
	}
}

func TestRemoveProject(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "herdlite.db"))
	projectPath := laravelProject(t, "my-app")

	project, err := store.AddProjectWithOptions(projectPath, ProjectOptions{PHPVersion: "8.4.1"})
	if err != nil {
		t.Fatal(err)
	}

	removed, err := store.RemoveProject(project.Name)
	if err != nil {
		t.Fatal(err)
	}
	if removed.Name != project.Name {
		t.Fatalf("expected removed project %s, got %s", project.Name, removed.Name)
	}

	if _, ok, err := store.ProjectByNameOrDomain(project.Name); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("expected project to be removed")
	}
}

func TestProjectResourcesCanBeOrphanedAndForgotten(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.db"))
	projectPath := laravelProject(t, "my-app")
	project, err := store.AddProjectWithOptions(projectPath, ProjectOptions{
		PHPVersion:       "8.4.1",
		WebsocketEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.RecordProjectResource(project, "my_app"); err != nil {
		t.Fatal(err)
	}
	if err := store.OrphanProjectResource(project.Name); err != nil {
		t.Fatal(err)
	}

	resources, err := store.ProjectResources()
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 {
		t.Fatalf("expected one resource, got %d", len(resources))
	}
	if resources[0].Linked {
		t.Fatal("expected resource to be orphaned")
	}
	if resources[0].DatabaseName != "my_app" || resources[0].Domain != project.Domain || resources[0].WebsocketDomain != project.Websocket.Domain {
		t.Fatalf("unexpected resource: %+v", resources[0])
	}

	if err := store.ForgetProjectResource(project.Name); err != nil {
		t.Fatal(err)
	}
	resources, err = store.ProjectResources()
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 0 {
		t.Fatalf("expected resources to be forgotten, got %+v", resources)
	}
}

func TestAddProjectWithCustomNameAndDomain(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "herdlite.db"))
	projectPath := laravelProject(t, "directory-name")

	project, err := store.AddProjectWithOptions(projectPath, ProjectOptions{
		Name:       "Customer Portal",
		Domain:     "portal.internal.test",
		PHPVersion: "8.4.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if project.Name != "customer-portal" {
		t.Fatalf("expected custom sanitized name, got %s", project.Name)
	}
	if project.Domain != "portal.internal.test" {
		t.Fatalf("expected custom domain, got %s", project.Domain)
	}
	if project.Websocket.Domain != "ws.portal.internal.test" {
		t.Fatalf("expected websocket domain to follow custom domain, got %s", project.Websocket.Domain)
	}
}

func TestRelinkProjectCanRenameByPath(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "herdlite.db"))
	projectPath := laravelProject(t, "app")

	project, err := store.AddProjectWithOptions(projectPath, ProjectOptions{PHPVersion: "8.4.1"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.AddProjectWithOptions(projectPath, ProjectOptions{
		Name:             "Renamed App",
		Domain:           "renamed.test",
		PHPVersion:       "8.5.1",
		WebsocketEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name == project.Name {
		t.Fatalf("expected project name to change from %s", project.Name)
	}
	if updated.Name != "renamed-app" || updated.Domain != "renamed.test" {
		t.Fatalf("expected renamed project, got %s / %s", updated.Name, updated.Domain)
	}
	if updated.Websocket.Domain != "ws.renamed.test" {
		t.Fatalf("expected renamed websocket domain, got %s", updated.Websocket.Domain)
	}

	_, found, err := store.ProjectByNameOrDomain(project.Name)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatalf("old project name %s should not resolve after rename", project.Name)
	}
}

func TestPHPRuntimeForRequestResolvesMinorToLatestInstalled(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "herdlite.db"))
	for _, version := range []string{"8.4.1", "8.4.9", "8.3.20"} {
		if err := store.UpsertPHPRuntime(PHPRuntime{
			Version:      version,
			Minor:        version[:3],
			Tag:          "php-" + version,
			Source:       "php.net",
			SourceURL:    "https://www.php.net/distributions/php-" + version + ".tar.gz",
			Prefix:       filepath.Join(t.TempDir(), version),
			PHPBinary:    "php",
			PHPFPMBinary: "php-fpm",
			InstalledAt:  time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	runtime, ok, err := store.PHPRuntimeForRequest("8.4")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected runtime")
	}
	if runtime.Version != "8.4.9" {
		t.Fatalf("expected 8.4.9, got %s", runtime.Version)
	}
}

func TestMailMessageStorageAndProjectClear(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "herdlite.db"))

	firstID, err := store.AddMailMessage(MailMessage{
		ProjectName: "app",
		Sender:      "noreply@app.test",
		Recipients:  "user@test",
		Subject:     "Welcome",
		TextBody:    "Hello",
		RawMIME:     []byte("Subject: Welcome\r\n\r\nHello"),
		Attachments: []MailAttachment{{
			Filename:    "notes.txt",
			ContentType: "text/plain",
			Size:        5,
			Content:     []byte("notes"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMailMessage(MailMessage{
		ProjectName: UnknownProjectName,
		Sender:      "dev@example.com",
		Recipients:  "user@test",
		Subject:     "Unknown",
		RawMIME:     []byte("Subject: Unknown\r\n\r\n"),
	}); err != nil {
		t.Fatal(err)
	}

	message, found, err := store.MailMessage(firstID)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected stored message")
	}
	if len(message.Attachments) != 1 {
		t.Fatalf("expected attachment, got %d", len(message.Attachments))
	}
	attachment, found, err := store.MailAttachment(firstID, message.Attachments[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected stored attachment")
	}
	if string(attachment.Content) != "notes" {
		t.Fatalf("expected attachment content, got %q", string(attachment.Content))
	}
	deleted, err := store.DeleteMailMessage(firstID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("expected deleted mail count 1, got %d", deleted)
	}
	if _, found, err := store.MailAttachment(firstID, message.Attachments[0].ID); err != nil {
		t.Fatal(err)
	} else if found {
		t.Fatal("expected attachment to be deleted with message")
	}

	projectMessages, err := store.MailMessages(MailFilter{ProjectName: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(projectMessages) != 0 {
		t.Fatalf("expected deleted app message to be gone, got %d", len(projectMessages))
	}

	cleared, err := store.ClearMailMessages(MailFilter{ProjectName: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if cleared != 0 {
		t.Fatalf("expected no app messages left to clear, got %d", cleared)
	}
	remaining, err := store.MailMessages(MailFilter{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].ProjectName != UnknownProjectName {
		t.Fatalf("expected only unknown message remaining, got %#v", remaining)
	}
}

func TestDebugDumpHardCapAndClearBefore(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "herdlite.db"))

	var cutoff int64
	for i := 0; i < MaxDebugDumps; i++ {
		id, err := store.AddDebugDump(DebugDump{ProjectName: "app", HTML: "<div>dump</div>"})
		if err != nil {
			t.Fatal(err)
		}
		if id == 0 {
			t.Fatalf("expected dump %d to be stored before cap", i)
		}
		if i == 10 {
			cutoff = id
		}
	}
	id, err := store.AddDebugDump(DebugDump{ProjectName: "app", HTML: "<div>overflow</div>"})
	if err != nil {
		t.Fatal(err)
	}
	if id != 0 {
		t.Fatalf("expected dump over cap to be silently skipped, got id %d", id)
	}

	cleared, err := store.ClearDebugDumpsBefore(cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if cleared != 10 {
		t.Fatalf("expected 10 old dumps cleared, got %d", cleared)
	}
	dumps, err := store.DebugDumps(MaxDebugDumps)
	if err != nil {
		t.Fatal(err)
	}
	if len(dumps) != MaxDebugDumps-10 {
		t.Fatalf("expected remaining dumps after clear-before, got %d", len(dumps))
	}
	deleted, err := store.DeleteDebugDump(cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("expected one deleted dump, got %d", deleted)
	}
}

func laravelProject(t *testing.T, name string) string {
	t.Helper()
	projectPath := filepath.Join(t.TempDir(), name)
	publicPath := filepath.Join(projectPath, "public")
	if err := os.MkdirAll(publicPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publicPath, "index.php"), []byte("<?php\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return projectPath
}
