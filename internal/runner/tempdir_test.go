package runner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewTempDir_and_OverwriteFile(t *testing.T) {
	// Create a temporary "module" directory with a file
	moduleDir := t.TempDir()

	subDir := filepath.Join(moduleDir, "pkg")
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatalf("creating subdir: %v", err)
	}

	originalContent := []byte("original content")
	if err := os.WriteFile(filepath.Join(subDir, "file.go"), originalContent, 0o644); err != nil {
		t.Fatalf("writing original file: %v", err)
	}

	// Create TempDir
	td, err := NewTempDir(moduleDir)
	if err != nil {
		t.Fatalf("NewTempDir: %v", err)
	}
	defer td.Cleanup()

	// Verify temp dir was created
	if td.Root == "" {
		t.Fatal("Root is empty")
	}
	if _, err := os.Stat(td.Root); err != nil {
		t.Fatalf("temp dir does not exist: %v", err)
	}

	// Overwrite the file
	newContent := []byte("overwritten content")
	if err := td.OverwriteFile(filepath.Join("pkg", "file.go"), newContent); err != nil {
		t.Fatalf("OverwriteFile: %v", err)
	}

	// Verify the overwritten content
	got, err := os.ReadFile(filepath.Join(td.Root, "pkg", "file.go"))
	if err != nil {
		t.Fatalf("reading overwritten file: %v", err)
	}
	if string(got) != string(newContent) {
		t.Errorf("content = %q, want %q", string(got), string(newContent))
	}

	// Cleanup and verify
	td.Cleanup()
	if _, err := os.Stat(td.Root); !os.IsNotExist(err) {
		t.Error("temp dir was not cleaned up")
	}
}
