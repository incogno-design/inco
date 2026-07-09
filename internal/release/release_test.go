package release

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/incogno-design/inco/internal/inco"
)

// writeCacheOverlay writes .inco_cache/overlay.json mapping the given
// source→shadow pairs, and creates each shadow file with content.
func writeCacheOverlay(t *testing.T, root string, replace map[string]string, shadows map[string]string) {
	t.Helper()
	cacheDir := filepath.Join(root, ".inco_cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for shadowPath, content := range shadows {
		if err := os.WriteFile(shadowPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(inco.Overlay{Replace: replace})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "overlay.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseRoundTrip(t *testing.T) {
	root := t.TempDir()
	origPath := filepath.Join(root, "foo.inco.go")
	origContent := "package p\n\nfunc F() {}\n"
	if err := os.WriteFile(origPath, []byte(origContent), 0o644); err != nil {
		t.Fatal(err)
	}
	shadowPath := filepath.Join(root, ".inco_cache", "foo_deadbeef.go")
	shadowContent := "package p\n\nfunc F() { _ = \"guarded\" }\n"
	writeCacheOverlay(t, root,
		map[string]string{origPath: shadowPath},
		map[string]string{shadowPath: shadowContent})

	// --- Release ---
	if err := Release(root, false); err != nil {
		t.Fatalf("Release: %v", err)
	}

	releasePath := filepath.Join(root, "foo.go")
	got, err := os.ReadFile(releasePath)
	if err != nil {
		t.Fatalf("released file missing: %v", err)
	}
	if !strings.HasPrefix(string(got), releaseHeader) {
		t.Errorf("released file missing generated-code header:\n%s", got)
	}
	if !strings.Contains(string(got), "guarded") {
		t.Errorf("released file should contain shadow content:\n%s", got)
	}
	if _, err := os.Stat(origPath); !os.IsNotExist(err) {
		t.Errorf("original .inco.go should have been renamed to backup")
	}
	backupPath := filepath.Join(root, "foo.inco")
	if _, err := os.Stat(backupPath); err != nil {
		t.Errorf("backup .inco missing: %v", err)
	}

	// --- ReleaseClean restores the original ---
	if err := ReleaseClean(root); err != nil {
		t.Fatalf("ReleaseClean: %v", err)
	}
	if _, err := os.Stat(releasePath); !os.IsNotExist(err) {
		t.Errorf("released .go should have been removed")
	}
	restored, err := os.ReadFile(origPath)
	if err != nil {
		t.Fatalf("original not restored: %v", err)
	}
	if string(restored) != origContent {
		t.Errorf("restored content mismatch:\ngot  %q\nwant %q", restored, origContent)
	}
}

func TestReleaseDryRunDoesNotModify(t *testing.T) {
	root := t.TempDir()
	origPath := filepath.Join(root, "foo.inco.go")
	if err := os.WriteFile(origPath, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	shadowPath := filepath.Join(root, ".inco_cache", "foo_deadbeef.go")
	writeCacheOverlay(t, root,
		map[string]string{origPath: shadowPath},
		map[string]string{shadowPath: "package p\n"})

	if err := Release(root, true); err != nil {
		t.Fatalf("Release dry-run: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "foo.go")); !os.IsNotExist(err) {
		t.Errorf("dry-run must not write the released .go file")
	}
	if _, err := os.Stat(origPath); err != nil {
		t.Errorf("dry-run must leave the original .inco.go in place: %v", err)
	}
}

func TestReleaseSkipsNonIncoGo(t *testing.T) {
	root := t.TempDir()
	// A plain .go source (not .inco.go) must be skipped entirely.
	origPath := filepath.Join(root, "plain.go")
	if err := os.WriteFile(origPath, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	shadowPath := filepath.Join(root, ".inco_cache", "plain_deadbeef.go")
	writeCacheOverlay(t, root,
		map[string]string{origPath: shadowPath},
		map[string]string{shadowPath: "package p\n"})

	if err := Release(root, false); err != nil {
		t.Fatalf("Release: %v", err)
	}

	// The original must be untouched and no backup created.
	if _, err := os.Stat(origPath); err != nil {
		t.Errorf("non-.inco.go source must be left untouched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "plain.inco")); !os.IsNotExist(err) {
		t.Errorf("no backup should be created for a non-.inco.go source")
	}
}
