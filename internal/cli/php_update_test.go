package cli

import (
	"bytes"
	"context"
	"errors"
	"herdlite/internal/paths"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"herdlite/internal/app"
	"herdlite/internal/phpmanager"
	"herdlite/internal/services"
	"herdlite/internal/state"
)

type fakeMaintenance struct {
	events          *[]string
	previous        services.MaintenanceState
	restored        services.MaintenanceState
	replacements    map[string]state.PHPRuntime
	pauseErr        error
	restoreFailures int
}

func (f *fakeMaintenance) CapturePHPState() (services.MaintenanceState, error) {
	*f.events = append(*f.events, "capture")
	return f.previous, nil
}
func (f *fakeMaintenance) PausePHP(_ context.Context, s services.MaintenanceState) error {
	*f.events = append(*f.events, "pause")
	return f.pauseErr
}
func (f *fakeMaintenance) RestorePHP(ctx context.Context, s services.MaintenanceState, r map[string]state.PHPRuntime) error {
	*f.events = append(*f.events, "restore")
	f.restored = s
	f.replacements = r
	if f.restoreFailures > 0 {
		f.restoreFailures--
		return errors.New("nginx activation failed")
	}
	return ctx.Err()
}

func batchPlans() []phpmanager.UpdatePlan {
	return []phpmanager.UpdatePlan{
		{Minor: "8.3", Old: []state.PHPRuntime{{Version: "8.3.1"}}, Release: phpmanager.Release{Version: "8.3.2"}},
		{Minor: "8.5", Old: []state.PHPRuntime{{Version: "8.5.1"}}, Release: phpmanager.Release{Version: "8.5.2"}},
	}
}

func TestUpdateBatchFailureContinueAndRestore(t *testing.T) {
	for _, cont := range []bool{false, true} {
		var out bytes.Buffer
		var events []string
		m := &fakeMaintenance{events: &events, previous: services.MaintenanceState{Nginx: true, FPM: []state.PHPRuntime{{Version: "8.3.1"}}}}
		code := executePHPUpdates(context.Background(), &app.App{Out: &out, Err: &out}, batchPlans(), m, func(_ context.Context, p phpmanager.UpdatePlan) (*phpmanager.UpdateResult, error) {
			events = append(events, p.Minor)
			if p.Minor == "8.3" {
				return nil, errors.New("build failed; see build.log")
			}
			return &phpmanager.UpdateResult{Runtime: state.PHPRuntime{Version: p.Release.Version}}, nil
		}, func(string) bool { events = append(events, "prompt"); return cont })
		want := []string{"capture", "pause", "8.3", "prompt"}
		if cont {
			want = append(want, "8.5")
		}
		want = append(want, "restore")
		if code != 1 || !reflect.DeepEqual(events, want) {
			t.Fatalf("code=%d events=%v", code, events)
		}
		if !reflect.DeepEqual(m.previous, m.restored) {
			t.Fatal("lost previous running state")
		}
		if !strings.Contains(out.String(), "build.log") {
			t.Fatal("missing failure detail")
		}
		if cont && m.replacements["8.5.1"].Version != "8.5.2" {
			t.Fatal("missing service replacement mapping")
		}
	}
}

func TestUpdateInterruptionAndStopFailureRestore(t *testing.T) {
	for _, stopFails := range []bool{false, true} {
		var out bytes.Buffer
		var events []string
		ctx, cancel := context.WithCancel(context.Background())
		m := &fakeMaintenance{events: &events}
		if stopFails {
			m.pauseErr = errors.New("stop failed")
		} else {
			cancel()
		}
		code := executePHPUpdates(ctx, &app.App{Out: &out, Err: &out}, batchPlans(), m, func(context.Context, phpmanager.UpdatePlan) (*phpmanager.UpdateResult, error) {
			t.Fatal("build should not run")
			return nil, nil
		}, func(string) bool { t.Fatal("prompt should not run"); return false })
		cancel()
		if code != 1 || events[len(events)-1] != "restore" {
			t.Fatalf("restoration skipped: %v", events)
		}
		if strings.Contains(out.String(), "Service restoration failed") {
			t.Fatal("restoration used cancelled context")
		}
	}
}

func TestUpdatePromptAndOptionValidation(t *testing.T) {
	var out bytes.Buffer
	prompt := updatePrompt(context.Background(), &out, strings.NewReader("yes\nn\n"))
	if !prompt("first") || prompt("second") {
		t.Fatal("buffered answers lost")
	}
	for _, args := range [][]string{{"--wat"}, {"8.3", "8.5"}} {
		if _, err := parsePHPUpdateOptions(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	options, err := parsePHPUpdateOptions([]string{"--dry-run", "8.5", "--force"})
	if err != nil || !options.dryRun || !options.force || options.minor != "8.5" {
		t.Fatal(options, err)
	}
}

func TestLockedDryRunWorksOfflineWithoutMutation(t *testing.T) {
	p := paths.ResolveForHome(t.TempDir())
	store := state.NewStore(p.StateFile)
	if err := store.UpsertPHPRuntime(state.PHPRuntime{Version: "8.5.11", Minor: "8.5"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetPHPLocked("8.5.11", true); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(p.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	a := &app.App{Paths: p, Store: store, Out: &out, Err: &out}
	if code := runPHP(a, []string{"update", "8.5", "--dry-run", "--force"}); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "8.5.11: locked") {
		t.Fatal(out.String())
	}
	after, err := os.ReadFile(p.StateFile)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("dry run mutated state")
	}
	if _, err := os.Stat(p.RuntimeDir); !os.IsNotExist(err) {
		t.Fatal("dry run created runtime directory")
	}
	if _, err := os.Stat(filepath.Join(p.ConfigDir, "extensions.toml")); !os.IsNotExist(err) {
		t.Fatal("dry run created manifest")
	}
	out.Reset()
	if code := runPHP(a, []string{"list"}); code != 0 || !strings.Contains(out.String(), "true") {
		t.Fatal(out.String())
	}
}

func TestActivationFailureRollsBackThenRestoresPreviousServices(t *testing.T) {
	var out bytes.Buffer
	var events []string
	m := &fakeMaintenance{events: &events, restoreFailures: 1, previous: services.MaintenanceState{Nginx: true}}
	code := executePHPUpdates(context.Background(), &app.App{Out: &out, Err: &out}, batchPlans(), m, func(_ context.Context, p phpmanager.UpdatePlan) (*phpmanager.UpdateResult, error) {
		events = append(events, p.Minor)
		return &phpmanager.UpdateResult{Runtime: state.PHPRuntime{Version: p.Release.Version}}, nil
	}, func(string) bool { t.Fatal("unexpected prompt"); return false })
	want := []string{"capture", "pause", "8.3", "8.5", "restore", "capture", "pause", "restore"}
	if code != 1 || !reflect.DeepEqual(events, want) {
		t.Fatalf("exit %d events %v", code, events)
	}
	if m.replacements != nil || !m.restored.Nginx {
		t.Fatal("did not restore original services after rollback")
	}
	if !strings.Contains(out.String(), "Updates rolled back") {
		t.Fatal(out.String())
	}
}
