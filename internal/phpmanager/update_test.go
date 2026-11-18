package phpmanager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"herdlite/internal/paths"
	"herdlite/internal/state"
)

func TestSelectUpdatesLocksAndMinorBoundaries(t *testing.T) {
	runtimes := []state.PHPRuntime{{Version: "8.5.11", Minor: "8.5"}, {Version: "8.5.10", Minor: "8.5"}, {Version: "8.3.1", Minor: "8.3"}}
	releases := []Release{{Version: "8.5.12", Minor: "8.5"}, {Version: "8.3.9", Minor: "8.3"}}
	plans, err := SelectUpdates(runtimes, map[string]bool{"8.5.11": true}, releases, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 || plans[0].Minor != "8.3" || plans[1].Old[0].Version != "8.5.10" || plans[1].Locked[0].Version != "8.5.11" {
		t.Fatalf("unexpected plans: %+v", plans)
	}
	plans, err = SelectUpdates(runtimes, map[string]bool{"8.5.11": true, "8.5.10": true}, nil, "8.5", true)
	if err != nil || plans[0].Needed() {
		t.Fatalf("locked minor should work offline and skip force: %+v %v", plans, err)
	}
	for _, minor := range []string{"8.5.11", "latest", "8.2"} {
		if _, err := SelectUpdates(runtimes, nil, releases, minor, false); err == nil {
			t.Fatalf("accepted %s", minor)
		}
	}
	if _, err := SelectUpdates(runtimes, nil, []Release{{Version: "8.5.9", Minor: "8.5"}}, "8.5", false); err == nil {
		t.Fatal("allowed downgrade")
	}
}

func TestSelectUpdatesReusesLockedLatest(t *testing.T) {
	runtimes := []state.PHPRuntime{{Version: "8.5.11", Minor: "8.5"}, {Version: "8.5.10", Minor: "8.5"}}
	plans, err := SelectUpdates(runtimes, map[string]bool{"8.5.11": true}, []Release{{Version: "8.5.11", Minor: "8.5"}}, "", true)
	if err != nil {
		t.Fatal(err)
	}
	p := plans[0]
	if !p.Needed() || p.Force || p.Target.Version != "8.5.11" || len(p.Old) != 1 {
		t.Fatalf("bad locked target: %+v", p)
	}
}

func updateFixture(t *testing.T, versions ...string) (Installer, []state.PHPRuntime) {
	t.Helper()
	p := paths.ResolveForHome(t.TempDir())
	if err := p.EnsureUserDirs(); err != nil {
		t.Fatal(err)
	}
	i := Installer{Paths: p, Store: state.NewStore(p.StateFile)}
	var runtimes []state.PHPRuntime
	for _, v := range versions {
		r := runtimeRecord(Release{Version: v, Minor: minorVersion(v)}, filepath.Join(p.PHPDir, v))
		for _, dir := range []string{"bin", "sbin", "etc", "var/sessions"} {
			if err := os.MkdirAll(filepath.Join(r.Prefix, dir), 0755); err != nil {
				t.Fatal(err)
			}
		}
		// A tiny executable fixture exercises actual version/module/FPM validation.
		script := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\n-m) printf '%%s\\n' %s;;\n-r) case \"$2\" in 'echo PHP_VERSION;') printf '%%s' '%s';; esac;;\nesac\n", strings.Join(requiredModulesForShell(), " "), v)
		if err := os.WriteFile(r.PHPBinary, []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(r.PHPFPMBinary, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(r.Prefix, "etc/php-fpm.conf"), []byte("pid="+r.Prefix+"/var/run/php-fpm.pid\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := i.Store.UpsertPHPRuntime(r); err != nil {
			t.Fatal(err)
		}
		runtimes = append(runtimes, r)
	}
	return i, runtimes
}
func requiredModulesForShell() []string {
	var out []string
	for _, m := range requiredModules {
		out = append(out, "'"+m+"'")
	}
	return out
}

func TestUpdatePlanDryRunAndConfigurationConflicts(t *testing.T) {
	i, r := updateFixture(t, "8.5.10", "8.5.11")
	releases := []Release{{Version: "8.5.12", Minor: "8.5"}}
	plans, err := i.PlanUpdates(releases, "", false)
	if err != nil || plans[0].Problem != "" {
		t.Fatalf("plan: %+v %v", plans, err)
	}
	if _, err := os.Stat(i.ExtensionConfigPath()); !os.IsNotExist(err) {
		t.Fatal("planning created extension manifest")
	}
	if err := os.WriteFile(filepath.Join(r[0].Prefix, "etc/custom.ini"), []byte("memory_limit=1G"), 0600); err != nil {
		t.Fatal(err)
	}
	plans, err = i.PlanUpdates(releases, "", false)
	if err != nil || !strings.Contains(plans[0].Problem, "conflicting PHP configuration") {
		t.Fatalf("missing conflict: %+v %v", plans, err)
	}
	if err := i.Store.SetPHPLocked(r[0].Version, true); err != nil {
		t.Fatal(err)
	}
	plans, err = i.PlanUpdates(releases, "", false)
	if err != nil || plans[0].Problem != "" {
		t.Fatalf("locked config should be excluded: %+v %v", plans, err)
	}
}

func TestUpdateExtensionConflict(t *testing.T) {
	i, _ := updateFixture(t, "8.5.10", "8.5.11")
	data := `[php."8.5.10"]
extensions=[]
[[php."8.5.11".extensions]]
name="grpc"
package="grpc/grpc-php-ext"
version="1.84.0"
`
	if err := os.WriteFile(i.ExtensionConfigPath(), []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	plans, err := i.PlanUpdates([]Release{{Version: "8.5.12", Minor: "8.5"}}, "", false)
	if err != nil || !strings.Contains(plans[0].Problem, "conflicting extension") {
		t.Fatalf("expected conflict: %+v %v", plans, err)
	}
}

func TestFailedBuildRestoresForcedRuntimeAndManifest(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprint(force), func(t *testing.T) {
			i, old := updateFixture(t, "8.5.11")
			version := "8.5.12"
			if force {
				version = "8.5.11"
			}
			plans, err := i.PlanUpdates([]Release{{Version: version, Minor: "8.5"}}, "", force)
			if err != nil {
				t.Fatal(err)
			}
			original, err := os.ReadFile(old[0].PHPBinary)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			result, err := i.applyUpdate(context.Background(), plans[0], false, false, func(ctx context.Context, o InstallOptions) (state.PHPRuntime, error) {
				calls++
				if o.Release.Version != version || !o.DeferRegister || !o.Force {
					t.Fatalf("bad build options: %+v", o)
				}
				prefix := filepath.Join(i.Paths.PHPDir, version)
				if err := os.MkdirAll(prefix, 0755); err != nil {
					t.Fatal(err)
				}
				return state.PHPRuntime{}, errors.New("compiler failed")
			})
			if err == nil || result != nil || calls != 1 {
				t.Fatalf("failure recovery: %+v %v calls=%d", result, err, calls)
			}
			restored, err := os.ReadFile(old[0].PHPBinary)
			if err != nil || !reflect.DeepEqual(original, restored) {
				t.Fatalf("runtime not restored: %v", err)
			}
			runtimes, err := i.Store.PHPRuntimes()
			if err != nil || len(runtimes) != 1 || runtimes[0].Version != old[0].Version {
				t.Fatal("registration changed")
			}
			if _, err := os.Stat(i.ExtensionConfigPath()); !os.IsNotExist(err) {
				t.Fatal("manifest not restored")
			}
			if !force {
				if _, err := os.Stat(filepath.Join(i.Paths.PHPDir, version)); !os.IsNotExist(err) {
					t.Fatal("partial install retained")
				}
			}
		})
	}
}

func TestReuseTargetMigrationAndRollback(t *testing.T) {
	i, r := updateFixture(t, "8.5.10", "8.5.11")
	if err := i.Store.SetMetaValue("global_php_version", r[0].Version); err != nil {
		t.Fatal(err)
	}
	plans, err := i.PlanUpdates([]Release{{Version: "8.5.11", Minor: "8.5"}}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	result, err := i.ApplyUpdate(context.Background(), plans[0], false, false)
	if err != nil {
		t.Fatal(err)
	}
	global, _, _ := i.Store.MetaValue("global_php_version")
	if global != "8.5.11" {
		t.Fatal(global)
	}
	if _, err := os.Stat(r[0].Prefix); err != nil {
		t.Fatal("old runtime removed before service restoration")
	}
	if err := result.Rollback(); err != nil {
		t.Fatal(err)
	}
	global, _, _ = i.Store.MetaValue("global_php_version")
	if global != "8.5.10" {
		t.Fatal(global)
	}
	runtimes, _ := i.Store.PHPRuntimes()
	if len(runtimes) != 2 {
		t.Fatal("rollback registrations missing")
	}
	result, err = i.ApplyUpdate(context.Background(), plans[0], false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r[0].Prefix); !os.IsNotExist(err) {
		t.Fatal("old prefix remains")
	}
	if _, err := os.Stat(r[1].Prefix); err != nil {
		t.Fatal("target removed")
	}
}

func TestUpdateRechecksLocks(t *testing.T) {
	i, r := updateFixture(t, "8.5.10")
	plans, err := i.PlanUpdates([]Release{{Version: "8.5.11", Minor: "8.5"}}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := i.Store.SetPHPLocked(r[0].Version, true); err != nil {
		t.Fatal(err)
	}
	_, err = i.ApplyUpdate(context.Background(), plans[0], false, false)
	if err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("lock ignored: %v", err)
	}
}

func TestForcedRebuildPreservesConfigModesAndCanRollback(t *testing.T) {
	i, r := updateFixture(t, "8.5.11")
	configPath := filepath.Join(r[0].Prefix, "etc/private.ini")
	if err := os.WriteFile(configPath, []byte("session.save_path="+r[0].Prefix+"/var/sessions\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r[0].Prefix, "var/sessions/sess_test"), []byte("session data"), 0600); err != nil {
		t.Fatal(err)
	}
	plans, err := i.PlanUpdates([]Release{{Version: "8.5.11", Minor: "8.5"}}, "", true)
	if err != nil {
		t.Fatal(err)
	}
	result, err := i.applyUpdate(context.Background(), plans[0], false, false, func(ctx context.Context, o InstallOptions) (state.PHPRuntime, error) {
		runtime := runtimeRecord(*o.Release, r[0].Prefix)
		if err := o.Configure(runtime); err != nil {
			return state.PHPRuntime{}, err
		}
		if err := os.MkdirAll(filepath.Dir(runtime.PHPBinary), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(runtime.PHPBinary, []byte("new binary"), 0755); err != nil {
			t.Fatal(err)
		}
		return runtime, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(configPath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("config mode lost", err)
	}
	data, err := os.ReadFile(filepath.Join(r[0].Prefix, "var/sessions/sess_test"))
	if err != nil || string(data) != "session data" {
		t.Fatal("sessions lost", err)
	}
	if err := result.Rollback(); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(r[0].PHPBinary)
	if err != nil || !strings.Contains(string(data), "#!/bin/sh") {
		t.Fatal("old binary not recovered")
	}
}

func TestLockedTargetFilesRemainUntouched(t *testing.T) {
	i, r := updateFixture(t, "8.5.10", "8.5.11")
	if err := i.Store.SetPHPLocked("8.5.11", true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r[0].Prefix, "var/sessions/sess_old"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := runtimeConfig(r[1].Prefix)
	if err != nil {
		t.Fatal(err)
	}
	plans, err := i.PlanUpdates([]Release{{Version: "8.5.11", Minor: "8.5"}}, "", true)
	if err != nil {
		t.Fatal(err)
	}
	result, err := i.ApplyUpdate(context.Background(), plans[0], false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Cleanup(); err != nil {
		t.Fatal(err)
	}
	after, err := runtimeConfig(r[1].Prefix)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("locked configuration changed")
	}
	if _, err := os.Stat(filepath.Join(r[1].Prefix, "var/sessions/sess_old")); !os.IsNotExist(err) {
		t.Fatal("locked session directory modified")
	}
	if locked, err := i.Store.PHPLocked("8.5.11"); err != nil || !locked {
		t.Fatal("target lock lost")
	}
}

func TestPHPChangeLockIsExclusive(t *testing.T) {
	p := paths.ResolveForHome(t.TempDir())
	release, err := LockChanges(p)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := LockChanges(p); err == nil {
		second()
		t.Fatal("concurrent changes allowed")
	}
	release()
	second, err := LockChanges(p)
	if err != nil {
		t.Fatal(err)
	}
	second()
}
