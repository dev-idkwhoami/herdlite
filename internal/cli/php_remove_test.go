package cli

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"herdlite/internal/app"
	"herdlite/internal/phpmanager"
	"herdlite/internal/services"
	"herdlite/internal/state"
)

func TestRemovalChoicePrompts(t *testing.T) {
	choices := []state.PHPRuntime{{Version: "8.5.11"}, {Version: "8.3.9"}}
	var out bytes.Buffer
	read := removalReader(context.Background(), strings.NewReader("invalid\n2\n8.5.11\nq\n"))
	version, err := chooseRemovalTarget(&out, read, "first", choices)
	if err != nil || version != "8.3.9" {
		t.Fatal(version, err)
	}
	version, err = chooseRemovalTarget(&out, read, "second", choices)
	if err != nil || version != "8.5.11" {
		t.Fatal(version, err)
	}
	if _, err = chooseRemovalTarget(&out, read, "third", choices); err == nil {
		t.Fatal("q did not cancel")
	}
	if !strings.Contains(out.String(), "Choose one of") {
		t.Fatal("invalid choice not explained")
	}
}

func TestRemovalServiceStatePreservesOfflineAndReplacesActiveFPM(t *testing.T) {
	p := phpmanager.RemovalPlan{Before: state.PHPUpdateState{Old: []state.PHPRuntime{{Version: "8.5.6"}}, Projects: []state.Project{{Name: "one", Enabled: true}, {Name: "two", Enabled: true}}}, Choices: []state.PHPRuntime{{Version: "8.5.11"}, {Version: "8.3.9"}}, Replacements: map[string]string{"one": "8.5.11", "two": "8.5.11"}}
	after := removalServiceState(services.MaintenanceState{}, p)
	if len(after.FPM) != 0 || after.Nginx || after.Daemon {
		t.Fatal("started inactive services")
	}
	before := services.MaintenanceState{Nginx: true, FPM: []state.PHPRuntime{{Version: "8.5.6"}, {Version: "8.4.1"}}}
	after = removalServiceState(before, p)
	var versions []string
	for _, r := range after.FPM {
		versions = append(versions, r.Version)
	}
	if !reflect.DeepEqual(versions, []string{"8.4.1", "8.5.11"}) || !after.Nginx {
		t.Fatal(after)
	}
	if before.FPM[0].Version != "8.5.6" {
		t.Fatal("modified rollback snapshot")
	}
}

func TestRemovalFailureRestoresServices(t *testing.T) {
	var out bytes.Buffer
	var events []string
	m := &fakeMaintenance{events: &events, previous: services.MaintenanceState{Nginx: true}}
	p := phpmanager.RemovalPlan{Before: state.PHPUpdateState{Old: []state.PHPRuntime{{Version: "8.5.6"}}}}
	code := executePHPRemoval(context.Background(), &app.App{Out: &out, Err: &out}, p, m, func(context.Context, phpmanager.RemovalPlan) (*phpmanager.UpdateResult, error) {
		events = append(events, "remove")
		return nil, errors.New("state failed")
	})
	if code != 1 || !reflect.DeepEqual(events, []string{"capture", "pause", "remove", "restore"}) || !m.restored.Nginx {
		t.Fatal(code, events)
	}
}

func TestRemovalActivationFailureRollsBack(t *testing.T) {
	var out bytes.Buffer
	var events []string
	m := &fakeMaintenance{events: &events, restoreFailures: 1, previous: services.MaintenanceState{Nginx: true}}
	p := phpmanager.RemovalPlan{Before: state.PHPUpdateState{Old: []state.PHPRuntime{{Version: "8.5.6"}}}}
	code := executePHPRemoval(context.Background(), &app.App{Out: &out, Err: &out}, p, m, func(context.Context, phpmanager.RemovalPlan) (*phpmanager.UpdateResult, error) {
		return &phpmanager.UpdateResult{}, nil
	})
	if code != 1 || !strings.Contains(out.String(), "Removal rolled back.") || !reflect.DeepEqual(events, []string{"capture", "pause", "restore", "capture", "pause", "restore"}) {
		t.Fatal(code, events, out.String())
	}
}

func TestRemovalSelectsOneReplacementForAllReferences(t *testing.T) {
	for _, projects := range [][]state.Project{nil, {{Name: "one"}, {Name: "two"}}} {
		p := phpmanager.RemovalPlan{Before: state.PHPUpdateState{Old: []state.PHPRuntime{{Version: "8.5.6"}}, Projects: projects, Global: "8.5.6", HasGlobal: true}, Choices: []state.PHPRuntime{{Version: "8.5.11"}, {Version: "8.3.9"}}}
		var out bytes.Buffer
		calls := 0
		err := selectRemovalReplacement(&out, func() (string, error) { calls++; return "2", nil }, &p)
		if err != nil || calls != 1 || p.GlobalReplacement != "8.3.9" {
			t.Fatalf("calls=%d plan=%+v err=%v", calls, p, err)
		}
		for _, project := range projects {
			if p.Replacements[project.Name] != "8.3.9" {
				t.Fatal("project has a different target")
			}
		}
	}
}

func TestRemovalUnusedRuntimeNeedsNoReplacement(t *testing.T) {
	p := phpmanager.RemovalPlan{Before: state.PHPUpdateState{Old: []state.PHPRuntime{{Version: "8.5.6"}}}}
	var out bytes.Buffer
	if err := selectRemovalReplacement(&out, func() (string, error) { t.Fatal("unexpected replacement prompt"); return "", nil }, &p); err != nil {
		t.Fatal(err)
	}
}
