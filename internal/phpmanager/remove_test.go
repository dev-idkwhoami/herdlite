package phpmanager

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRemovalPlanAndLifecycle(t *testing.T) {
	i, r := updateFixture(t, "8.5.6", "8.5.11")
	if err := i.Store.SetMetaValue("global_php_version", "8.5.6"); err != nil {
		t.Fatal(err)
	}
	if _, err := i.PlanRemoval("8.5"); err == nil {
		t.Fatal("accepted minor selector")
	}
	if _, err := i.PlanRemoval("8.5.99"); err == nil {
		t.Fatal("accepted missing version")
	}
	p, err := i.PlanRemoval("8.5.6")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Choices) != 1 || p.Choices[0].Version != "8.5.11" {
		t.Fatal(p.Choices)
	}
	p.GlobalReplacement = "8.5.11"
	result, err := i.ApplyRemoval(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r[0].Prefix); err != nil {
		t.Fatal("directory removed before service recovery")
	}
	if err := result.Rollback(); err != nil {
		t.Fatal(err)
	}
	if global, _, _ := i.Store.MetaValue("global_php_version"); global != "8.5.6" {
		t.Fatal("global not restored")
	}
	result, err = i.ApplyRemoval(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r[0].Prefix); !os.IsNotExist(err) {
		t.Fatal("old prefix remains")
	}
	if _, err := os.Stat(r[1].PHPBinary); err != nil {
		t.Fatal("replacement modified")
	}
}

func TestRemovalLastVersionAndLocks(t *testing.T) {
	i, _ := updateFixture(t, "8.5.6")
	if err := i.Store.SetPHPLocked("8.5.6", true); err != nil {
		t.Fatal(err)
	}
	if _, err := i.PlanRemoval("8.5.6"); err == nil {
		t.Fatal("accepted locked version")
	}
	if err := i.Store.SetPHPLocked("8.5.6", false); err != nil {
		t.Fatal(err)
	}
	if err := i.Store.SetMetaValue("global_php_version", "8.5.6"); err != nil {
		t.Fatal(err)
	}
	if _, err := i.PlanRemoval("8.5.6"); err == nil {
		t.Fatal("accepted last version while in use")
	}
	if err := i.Store.DeleteMetaValue("global_php_version"); err != nil {
		t.Fatal(err)
	}
	p, err := i.PlanRemoval("8.5.6")
	if err != nil {
		t.Fatal(err)
	}
	result, err := i.ApplyRemoval(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Cleanup(); err != nil {
		t.Fatal(err)
	}
}

func TestRemovalCancelledBeforeMutation(t *testing.T) {
	i, r := updateFixture(t, "8.5.6", "8.5.11")
	p, err := i.PlanRemoval("8.5.6")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = i.ApplyRemoval(ctx, p)
	if err == nil {
		t.Fatal("ignored cancellation")
	}
	if _, found, _ := i.Store.PHPRuntimeForRequest("8.5.6"); !found {
		t.Fatal("registration removed")
	}
	if _, err := os.Stat(filepath.Join(r[0].Prefix, "bin/php")); err != nil {
		t.Fatal(err)
	}
}
