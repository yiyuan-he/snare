package runner

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/yiyuanh/snare/internal/lang"
	"github.com/yiyuanh/snare/pkg/model"
)

// Executor runs generated tests against parent and new code to detect behavioral changes.
type Executor struct {
	moduleDir string
	lang      lang.Language
	timeout   time.Duration
	verbose   bool
}

// NewExecutor creates a new test executor.
func NewExecutor(moduleDir string, language lang.Language, timeout time.Duration, verbose bool) *Executor {
	return &Executor{
		moduleDir: moduleDir,
		lang:      language,
		timeout:   timeout,
		verbose:   verbose,
	}
}

// ExecuteCatching runs a test against parent (old) and new code to detect behavioral changes.
// Flow:
//  1. Run test with parent source — must pass (validates test correctness)
//  2. Run test with new source — if fails, it's a weak catch (behavioral change detected)
func (e *Executor) ExecuteCatching(test model.GeneratedTest, mutant model.Mutant, filePath string, parentSource []byte, newSource []byte) (model.TestResult, error) {
	result := model.TestResult{
		Test:   test,
		Mutant: mutant,
	}

	relPath, err := filepath.Rel(e.moduleDir, filePath)
	if err != nil {
		return result, fmt.Errorf("computing relative path: %w", err)
	}

	// Determine where to put the test file (same directory as the target file)
	testRelPath := filepath.Join(filepath.Dir(relPath), fmt.Sprintf("snare_%s_test.go", strings.ToLower(test.TestName)))

	// Step 1: Run test against parent (old) code — must pass
	td, err := NewTempDir(e.moduleDir)
	if err != nil {
		return result, fmt.Errorf("creating temp dir: %w", err)
	}
	defer td.Cleanup()

	// Overwrite the source file with parent version
	if err := td.OverwriteFile(relPath, parentSource); err != nil {
		return result, fmt.Errorf("writing parent source: %w", err)
	}

	// Write the test file
	if err := td.OverwriteFile(testRelPath, []byte(test.TestCode)); err != nil {
		return result, fmt.Errorf("writing test file: %w", err)
	}

	passed, output, err := e.lang.RunTest(td.Root, testRelPath, test.TestName, e.timeout)
	if err != nil {
		return result, fmt.Errorf("running test on parent: %w", err)
	}
	result.PassParent = passed
	result.ParentOutput = output

	if e.verbose {
		fmt.Printf("  [parent] %s: passed=%v\n", test.TestName, passed)
	}

	if !passed {
		// Test doesn't pass on parent code — not a valid catching test
		result.FilteredReason = "fails on parent code"
		return result, nil
	}

	// Step 2: Run test against new (diff) code — failure means behavioral change
	td2, err := NewTempDir(e.moduleDir)
	if err != nil {
		return result, fmt.Errorf("creating temp dir for new code: %w", err)
	}
	defer td2.Cleanup()

	// Overwrite the source file with new version
	if err := td2.OverwriteFile(relPath, newSource); err != nil {
		return result, fmt.Errorf("writing new source: %w", err)
	}

	// Write the test file
	if err := td2.OverwriteFile(testRelPath, []byte(test.TestCode)); err != nil {
		return result, fmt.Errorf("writing test file for new code: %w", err)
	}

	passed, output, err = e.lang.RunTest(td2.Root, testRelPath, test.TestName, e.timeout)
	if err != nil {
		return result, fmt.Errorf("running test on new code: %w", err)
	}
	result.FailDiff = !passed
	result.DiffOutput = output
	result.IsCatching = result.PassParent && result.FailDiff

	if e.verbose {
		fmt.Printf("  [new]    %s: passed=%v (catching=%v)\n", test.TestName, passed, result.IsCatching)
	}

	return result, nil
}
