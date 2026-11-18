package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNodeSourcesKeepNewestPerMajor(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []string{"v22.9.0", "v22.10.0", "v24.1.0", "v24.0.9", "v24.1.0-rc.1", "garbage"} {
		if err := os.MkdirAll(filepath.Join(dir, "versions", "node", v), 0755); err != nil {
			t.Fatal(err)
		}
	}
	got, err := NodeSources(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"v22.10.0", "v24.1.0"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
func TestPostgresMajorGuard(t *testing.T) {
	for _, tc := range []struct {
		plan, major string
		blocked     bool
	}{
		{"nginx 1.28.0-1\npostgresql 17.6-1\n", "17\n", false},
		{"postgresql 18.0-1\n", "17", true},
		{"postgresql 1:18.0-2\n", "18", false},
		{"postgresql 18.0-1\n", "", false},
		{"nginx 1.28.0-1\n", "17", false},
	} {
		if err := CheckPostgresTransaction(tc.plan, tc.major); (err != nil) != tc.blocked {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
}
func TestReleaseChecksum(t *testing.T) {
	data := []byte("binary")
	sum := sha256.Sum256(data)
	if err := verifyChecksum(data, fmt.Sprintf("%x  herdlite.tar.gz\n", sum)); err != nil {
		t.Fatal(err)
	}
	for _, checksum := range []string{"", "bad", strings.Repeat("0", 64)} {
		if verifyChecksum(data, checksum) == nil {
			t.Fatalf("accepted %q", checksum)
		}
	}
}
func TestArchiveOnlyExtractsExpectedRegularBinary(t *testing.T) {
	for _, kind := range []byte{tar.TypeReg, tar.TypeSymlink} {
		var archive bytes.Buffer
		gz := gzip.NewWriter(&archive)
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(&tar.Header{Name: "../../outside", Typeflag: tar.TypeReg, Size: 4, Mode: 0755}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte("evil"))
		h := &tar.Header{Name: "herdlite-linux-amd64/herdlite", Typeflag: kind, Mode: 0755, Linkname: "/bin/sh"}
		if kind == tar.TypeReg {
			h.Size = 4
		}
		_ = tw.WriteHeader(h)
		if kind == tar.TypeReg {
			_, _ = tw.Write([]byte("good"))
		}
		_ = tw.Close()
		_ = gz.Close()
		dest := filepath.Join(t.TempDir(), "staged")
		_ = os.WriteFile(dest, nil, 0600)
		err := extractBinary(archive.Bytes(), dest, h.Name)
		if kind == tar.TypeSymlink {
			if err == nil {
				t.Fatal("accepted symlink")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(dest)
		if string(got) != "good" {
			t.Fatalf("unexpected binary: %q", got)
		}
	}
}

func TestSelfUpdateRejectsDowngradesAndUnversionedBuilds(t *testing.T) {
	for _, tc := range []struct {
		current, target string
		allowed         bool
	}{
		{"v0.4.0", "v0.5.0", true}, {"v0.5.0", "v0.5.0", true},
		{"v0.5.0", "v0.4.0", false}, {"v1.0.0", "v0.99.99", false},
		{"v0.10.0", "v0.9.0", false}, {"v0.9.0", "v0.10.0", true},
		{"dev", "v0.5.0", false}, {"master", "v0.5.0", false},
		{"v0.5.0-dirty", "v0.5.0", false}, {"v0.5.0", "v0.6.0-rc.1", false},
		{"v0.5.0", "v00.6.0", false}, {"v0.5.0", "v0.+6.0", false},
	} {
		if err := checkSelfUpgrade(tc.current, tc.target); (err == nil) != tc.allowed {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	// A development build must be rejected before contacting GitHub or staging files.
	dir := t.TempDir()
	staged, _, err := StageSelf(context.Background(), filepath.Join(dir, "herdlite"), "dev")
	if err == nil || staged != "" || !strings.Contains(err.Error(), "preserves local/development") {
		t.Fatalf("staged=%q, err=%v", staged, err)
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 0 {
		t.Fatalf("development build was modified: %v", files)
	}
}
