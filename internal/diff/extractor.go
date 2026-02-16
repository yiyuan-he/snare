package diff

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/yiyuanh/snare/pkg/model"
)

// Extractor extracts and parses git diffs.
type Extractor struct {
	Dir string
}

// NewExtractor creates a new diff extractor for the given directory.
func NewExtractor(dir string) *Extractor {
	return &Extractor{Dir: dir}
}

// Extract runs the appropriate git diff command and parses the output.
// It filters results to only include .go files (excluding test files).
// It also retrieves the parent version of each changed file.
func (e *Extractor) Extract(staged bool, commit string) ([]model.FileDiff, error) {
	raw, err := e.runGitDiff(staged, commit)
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	diffs, err := e.parse(raw)
	if err != nil {
		return nil, err
	}

	// Fetch parent source for each file
	for i := range diffs {
		parentSrc, err := e.getParentSource(diffs[i].OldName, staged, commit)
		if err != nil {
			// Not fatal — file might be newly added (no parent)
			continue
		}
		diffs[i].ParentSource = parentSrc
	}

	return diffs, nil
}

func (e *Extractor) runGitDiff(staged bool, commit string) (string, error) {
	var args []string
	switch {
	case commit != "":
		args = []string{"diff-tree", "-p", "--root", commit}
	case staged:
		args = []string{"diff", "--cached"}
	default:
		args = []string{"diff"}
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = e.Dir
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%s: %s", strings.Join(args, " "), string(exitErr.Stderr))
		}
		return "", err
	}
	return string(out), nil
}

// getParentSource retrieves the file content at the parent revision.
func (e *Extractor) getParentSource(filePath string, staged bool, commit string) ([]byte, error) {
	if filePath == "" {
		return nil, fmt.Errorf("no old file path (new file)")
	}

	var args []string
	switch {
	case commit != "":
		// Parent of the specified commit
		args = []string{"show", commit + "^:" + filePath}
	default:
		// For both staged and unstaged changes, parent is HEAD
		args = []string{"show", "HEAD:" + filePath}
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = e.Dir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git show parent: %w", err)
	}
	return out, nil
}

func (e *Extractor) parse(raw string) ([]model.FileDiff, error) {
	files, _, err := gitdiff.Parse(strings.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parsing diff: %w", err)
	}

	var result []model.FileDiff
	for _, f := range files {
		name := f.NewName
		if name == "" {
			name = f.OldName
		}

		// Filter to .go files only, exclude test files
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			continue
		}

		fd := model.FileDiff{
			OldName: f.OldName,
			NewName: name,
		}

		for _, frag := range f.TextFragments {
			hunk := model.Hunk{
				OldStartLine: int(frag.OldPosition),
				OldLineCount: int(frag.OldLines),
				NewStartLine: int(frag.NewPosition),
				NewLineCount: int(frag.NewLines),
			}

			var lines []string
			for _, line := range frag.Lines {
				prefix := " "
				switch line.Op {
				case gitdiff.OpAdd:
					prefix = "+"
				case gitdiff.OpDelete:
					prefix = "-"
				}
				lines = append(lines, prefix+line.Line)
			}
			hunk.Content = strings.Join(lines, "\n")
			fd.Hunks = append(fd.Hunks, hunk)
		}

		if len(fd.Hunks) > 0 {
			// Resolve to absolute path for later file reading
			fd.NewName = filepath.Join(e.Dir, name)
			result = append(result, fd)
		}
	}
	return result, nil
}
