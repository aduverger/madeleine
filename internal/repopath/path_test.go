package repopath

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNormalizeRepositoryPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "relative", input: "src/main.go", want: "src/main.go"},
		{name: "nested cleaned", input: "src/generated/../main.go", want: "src/main.go"},
		{name: "absolute", input: filepath.Join(root, "Docs", "Plan.md"), want: "Docs/Plan.md"},
		{name: "case preserving", input: "MiXeD/File.Go", want: "MiXeD/File.Go"},
		{name: "dot-dot prefixed name", input: "..generated/schema.json", want: "..generated/schema.json"},
		{name: "empty", input: "", wantErr: true},
		{name: "root dot", input: ".", wantErr: true},
		{name: "absolute root", input: root, wantErr: true},
		{name: "relative outside", input: "../outside.go", wantErr: true},
		{name: "absolute outside", input: filepath.Join(filepath.Dir(root), "outside.go"), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := Normalize(root, test.input)
			if test.wantErr {
				if !errors.Is(err, ErrOutsideRepository) {
					t.Fatalf("error = %v, want ErrOutsideRepository", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalize path: %v", err)
			}
			if got != test.want {
				t.Fatalf("path = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeRepositoryPathPreservesSymlinkIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	t.Parallel()

	root := t.TempDir()
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}

	got, err := Normalize(root, filepath.Join(root, "linked", "secret.txt"))
	if err != nil {
		t.Fatalf("normalize symlink path: %v", err)
	}
	if got != "linked/secret.txt" {
		t.Fatalf("path = %q, want lexical symlink path", got)
	}
}
