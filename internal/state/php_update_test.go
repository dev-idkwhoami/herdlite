package state

import (
	"path/filepath"
	"testing"
)

func TestPHPLocksAndTransactionalReplacement(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "state.db"))
	old := PHPRuntime{Version: "8.5.10", Minor: "8.5"}
	locked := PHPRuntime{Version: "8.5.11", Minor: "8.5"}
	target := PHPRuntime{Version: "8.5.12", Minor: "8.5"}
	for _, r := range []PHPRuntime{old, locked} {
		if err := s.UpsertPHPRuntime(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetPHPLocked("8.5", true); err == nil {
		t.Fatal("accepted minor lock")
	}
	if err := s.SetPHPLocked("8.5.99", true); err == nil {
		t.Fatal("accepted missing lock")
	}
	if err := s.SetPHPLocked(locked.Version, true); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertPHPRuntime(locked); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.PHPLocked(locked.Version); err != nil || !ok {
		t.Fatal("upsert cleared lock")
	}
	p, err := s.AddProjectWithOptions(laravelProject(t, "unlocked"), ProjectOptions{PHPVersion: old.Version})
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := s.AddProjectWithOptions(laravelProject(t, "locked"), ProjectOptions{PHPVersion: locked.Version})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMetaValue("global_php_version", old.Version); err != nil {
		t.Fatal(err)
	}
	before := PHPUpdateState{Old: []PHPRuntime{old}, Projects: []Project{p}, Global: old.Version, HasGlobal: true}
	if err := s.ApplyPHPUpdate(before, target); err != nil {
		t.Fatal(err)
	}
	updated, _, _ := s.ProjectByNameOrDomain(p.Name)
	if updated.PHPVersion != target.Version {
		t.Fatal("project not migrated")
	}
	untouched, _, _ := s.ProjectByNameOrDomain(pinned.Name)
	if untouched.PHPVersion != locked.Version {
		t.Fatal("locked project changed")
	}
	if err := s.RestorePHPUpdate(before, target.Version); err != nil {
		t.Fatal(err)
	}
	updated, _, _ = s.ProjectByNameOrDomain(p.Name)
	if updated.PHPVersion != old.Version {
		t.Fatal("project not restored")
	}
	if err := s.ApplyPHPUpdate(PHPUpdateState{Old: []PHPRuntime{old, locked}}, target); err == nil {
		t.Fatal("transaction accepted locked removal")
	}
	if _, found, _ := s.PHPRuntimeForRequest(target.Version); found {
		t.Fatal("failed transaction registered target")
	}
	updated, _, _ = s.ProjectByNameOrDomain(p.Name)
	if updated.PHPVersion != old.Version {
		t.Fatal("failed transaction migrated project")
	}
	if err := s.SetPHPLocked(locked.Version, false); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.PHPLocked(locked.Version); ok {
		t.Fatal("unlock failed")
	}
}
