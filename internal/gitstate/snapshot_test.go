package gitstate

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWorktreeFingerprintIncludesPresenceModeContentAndSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	missing, err := fingerprintWorktreePath(root, "file")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "file", "first")
	first, err := fingerprintWorktreePath(root, "file")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "file", "second")
	second, err := fingerprintWorktreePath(root, "file")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "file"), 0o755); err != nil {
		t.Fatal(err)
	}
	executable, err := fingerprintWorktreePath(root, "file")
	if err != nil {
		t.Fatal(err)
	}
	if missing == first || first == second || second == executable {
		t.Fatalf("fingerprints do not distinguish missing, content, and mode: %q %q %q %q",
			missing, first, second, executable)
	}

	if err := os.Symlink("first-target", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	firstTarget, err := fingerprintWorktreePath(root, "link")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("second-target", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	secondTarget, err := fingerprintWorktreePath(root, "link")
	if err != nil {
		t.Fatal(err)
	}
	if firstTarget == secondTarget {
		t.Fatal("symlink target did not change fingerprint")
	}
}

func TestParsePorcelainStatusPreservesTwoPathRecords(t *testing.T) {
	entries, err := parsePorcelainStatus([]byte("R  new name\x00old\nname\x00C  copy\x00source\x00"))
	if err != nil {
		t.Fatal(err)
	}
	want := []gitStatusEntry{
		{Path: "new name", PorcelainStatus: "R "},
		{Path: "old\nname", PorcelainStatus: "R "},
		{Path: "copy", PorcelainStatus: "C "},
		{Path: "source", PorcelainStatus: "C "},
	}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries = %#v, want %#v", entries, want)
	}
}

func writeFile(t *testing.T, root, path, contents string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
