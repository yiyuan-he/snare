package runner

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/yiyuanh/snare/internal/lang"
	"github.com/yiyuanh/snare/pkg/model"
)

// Executor runs generated tests against original and mutated code.
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

// ExecuteWithFile runs a test against original and mutated code for a specific file.
func (e *Executor) ExecuteWithFile(test model.GeneratedTest, mutant model.Mutant, filePath string, originalSource []byte) (model.TestResult, error) {
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

	// Step 1: Run test against original code
	td, err := NewTempDir(e.moduleDir)
	if err != nil {
		return result, fmt.Errorf("creating temp dir: %w", err)
	}
	defer td.Cleanup()

	// Write the test file
	if err := td.OverwriteFile(testRelPath, []byte(test.TestCode)); err != nil {
		return result, fmt.Errorf("writing test file: %w", err)
	}

	passed, output, err := e.lang.RunTest(td.Root, testRelPath, test.TestName, e.timeout)
	if err != nil {
		return result, fmt.Errorf("running test on original: %w", err)
	}
	result.PassOriginal = passed
	result.OriginalOutput = output

	if e.verbose {
		fmt.Printf("  [original] %s: passed=%v\n", test.TestName, passed)
	}

	if !passed {
		// Test doesn't pass on original code — not a valid catching test
		result.FilteredReason = "fails on original code"
		return result, nil
	}

	// Step 2: Apply the mutant and run again
	mutatedSource, err := e.lang.ApplyMutant(originalSource, mutant.Original, mutant.Mutated)
	if err != nil {
		result.FilteredReason = fmt.Sprintf("cannot apply mutant: %v", err)
		return result, nil
	}

	// Create a new temp dir for the mutant run
	td2, err := NewTempDir(e.moduleDir)
	if err != nil {
		return result, fmt.Errorf("creating temp dir for mutant: %w", err)
	}
	defer td2.Cleanup()

	// Overwrite the source file with mutated version
	if err := td2.OverwriteFile(relPath, mutatedSource); err != nil {
		return result, fmt.Errorf("writing mutated source: %w", err)
	}

	// Write the test file
	if err := td2.OverwriteFile(testRelPath, []byte(test.TestCode)); err != nil {
		return result, fmt.Errorf("writing test file for mutant: %w", err)
	}

	passed, output, err = e.lang.RunTest(td2.Root, testRelPath, test.TestName, e.timeout)
	if err != nil {
		return result, fmt.Errorf("running test on mutant: %w", err)
	}
	result.FailMutant = !passed
	result.MutantOutput = output
	result.IsCatching = result.PassOriginal && result.FailMutant

	if e.verbose {
		fmt.Printf("  [mutant]   %s: passed=%v (catching=%v)\n", test.TestName, passed, result.IsCatching)
	}

	return result, nil
}
