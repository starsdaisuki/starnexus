package webassets

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestDistMatchesCanonicalSource fails when server/internal/webassets/dist
// has drifted from the canonical frontend in web/public. Run
// `make sync-web` to fix.
func TestDistMatchesCanonicalSource(t *testing.T) {
	source := filepath.Join("..", "..", "..", "web", "public")
	if _, err := os.Stat(source); err != nil {
		t.Skipf("canonical frontend %s not present (vendored build?): %v", source, err)
	}

	sourceFiles := map[string][]byte{}
	err := filepath.WalkDir(source, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if filepath.Base(path) == ".DS_Store" {
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sourceFiles[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", source, err)
	}

	embedded := FS()
	embeddedCount := 0
	err = fs.WalkDir(embedded, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		embeddedCount++
		want, ok := sourceFiles[path]
		if !ok {
			t.Errorf("embedded file %s no longer exists in web/public — run `make sync-web`", path)
			return nil
		}
		got, err := fs.ReadFile(embedded, path)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, want) {
			t.Errorf("embedded file %s differs from web/public — run `make sync-web`", path)
		}
		delete(sourceFiles, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walking embedded FS: %v", err)
	}

	for path := range sourceFiles {
		t.Errorf("web/public/%s is missing from the embedded copy — run `make sync-web`", path)
	}
	if embeddedCount == 0 {
		t.Fatal("embedded FS is empty")
	}
}
