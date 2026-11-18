package state

import (
	"path/filepath"
	"testing"
)

func TestRemovePHPWithDifferentProjectTargetsAndRollback(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "state.db"))
	old := PHPRuntime{Version: "8.5.6", Minor: "8.5"}
	for _, r := range []PHPRuntime{old, {Version: "8.5.11", Minor: "8.5"}, {Version: "8.3.9", Minor: "8.3"}} {
		if err := s.UpsertPHPRuntime(r); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.AddProjectWithOptions(laravelProject(t, "first"), ProjectOptions{PHPVersion: old.Version})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.AddProjectWithOptions(laravelProject(t, "second"), ProjectOptions{PHPVersion: old.Version})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMetaValue("global_php_version", old.Version); err != nil {
		t.Fatal(err)
	}
	before := PHPUpdateState{Old: []PHPRuntime{old}, Projects: []Project{first, second}, Global: old.Version, HasGlobal: true}
	replacements := map[string]string{first.Name: "8.5.11", second.Name: "8.3.9"}
	if err := s.ApplyPHPRemoval(before, replacements, "8.5.11"); err != nil {
		t.Fatal(err)
	}
	for name, target := range replacements {
		p, _, _ := s.ProjectByNameOrDomain(name)
		if p.PHPVersion != target {
			t.Fatalf("%s not migrated", name)
		}
	}
	if _, found, _ := s.PHPRuntimeForRequest(old.Version); found {
		t.Fatal("old runtime still registered")
	}
	if value, _, _ := s.MetaValue("global_php_version"); value != "8.5.11" {
		t.Fatal(value)
	}
	if err := s.RestorePHPUpdate(before, ""); err != nil {
		t.Fatal(err)
	}
	for name := range replacements {
		p, _, _ := s.ProjectByNameOrDomain(name)
		if p.PHPVersion != old.Version {
			t.Fatal("project not restored")
		}
	}
	runtimes, _ := s.PHPRuntimes()
	if len(runtimes) != 3 {
		t.Fatal("rollback removed existing replacement")
	}
	// Invalid selections must not leave a partially migrated project.
	replacements[second.Name] = "9.9.9"
	if err := s.ApplyPHPRemoval(before, replacements, "8.5.11"); err == nil {
		t.Fatal("accepted missing replacement")
	}
	p, _, _ := s.ProjectByNameOrDomain(first.Name)
	if p.PHPVersion != old.Version {
		t.Fatal("partial migration committed")
	}
	if err := s.SetPHPLocked(old.Version, true); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyPHPRemoval(before, replacements, "8.5.11"); err == nil {
		t.Fatal("removed locked runtime")
	}
}

func TestRemovalRejectsUnplannedReferences(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "state.db"))
	old := PHPRuntime{Version: "8.5.6", Minor: "8.5"}
	if err := s.UpsertPHPRuntime(old); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddProjectWithOptions(laravelProject(t, "new"), ProjectOptions{PHPVersion: old.Version}); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyPHPRemoval(PHPUpdateState{Old: []PHPRuntime{old}}, nil, ""); err == nil {
		t.Fatal("removed referenced runtime")
	}
	if _, found, _ := s.PHPRuntimeForRequest(old.Version); !found {
		t.Fatal("runtime unregistered")
	}
}
