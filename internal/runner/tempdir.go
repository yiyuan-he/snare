package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TempDir manages a temporary directory that mirrors the module root
// with symlinks, allowing isolated file modifications for testing.
type TempDir struct {
	Root      string // the temp directory root
	ModuleDir string // the original module directory
}

// NewTempDir creates a temporary directory and symlinks the module contents.
func NewTempDir(moduleDir string) (*TempDir, error) {
	tmpDir, err := os.MkdirTemp("", "snare-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}

	td := &TempDir{
		Root:      tmpDir,
		ModuleDir: moduleDir,
	}

	if err := td.symlinkContents(); err != nil {
		td.Cleanup()
		return nil, err
	}

	return td, nil
}

// symlinkContents creates symlinks for all top-level entries in the module dir.
func (td *TempDir) symlinkContents() error {
	entries, err := os.ReadDir(td.ModuleDir)
	if err != nil {
		return fmt.Errorf("reading module dir: %w", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		src := filepath.Join(td.ModuleDir, name)
		dst := filepath.Join(td.Root, name)
		if err := os.Symlink(src, dst); err != nil {
			return fmt.Errorf("symlinking %s: %w", name, err)
		}
	}
	return nil
}

// OverwriteFile replaces a symlink with an actual file containing the given content.
// The path should be relative to the module root.
func (td *TempDir) OverwriteFile(relPath string, content []byte) error {
	target := filepath.Join(td.Root, relPath)

	// Remove the symlink (or parent symlink if the file is nested)
	topLevel := strings.SplitN(relPath, string(filepath.Separator), 2)[0]
	topLevelPath := filepath.Join(td.Root, topLevel)

	info, err := os.Lstat(topLevelPath)
	if err != nil {
		return fmt.Errorf("stat %s: %w", topLevelPath, err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		// It's a symlink — we need to replace the entire directory tree
		// First, resolve the symlink to get the real path
		realPath, err := os.Readlink(topLevelPath)
		if err != nil {
			return fmt.Errorf("readlink %s: %w", topLevelPath, err)
		}

		realInfo, err := os.Stat(realPath)
		if err != nil {
			return fmt.Errorf("stat real path %s: %w", realPath, err)
		}

		if realInfo.IsDir() {
			// Remove the symlink and copy the directory
			if err := os.Remove(topLevelPath); err != nil {
				return fmt.Errorf("removing symlink %s: %w", topLevelPath, err)
			}
			if err := copyDir(realPath, topLevelPath); err != nil {
				return fmt.Errorf("copying dir %s: %w", realPath, err)
			}
		} else {
			// It's a file symlink — just remove and write
			if err := os.Remove(topLevelPath); err != nil {
				return fmt.Errorf("removing symlink %s: %w", topLevelPath, err)
			}
		}
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("creating parent dir: %w", err)
	}

	return os.WriteFile(target, content, 0o644)
}

// Cleanup removes the temporary directory.
func (td *TempDir) Cleanup() {
	os.RemoveAll(td.Root)
}

// copyDir recursively copies a directory, symlinking files for efficiency.
func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			// Symlink files for efficiency
			if err := os.Symlink(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}
