package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yiyuanh/snare/internal/assess"
	"github.com/yiyuanh/snare/internal/analysis"
	"github.com/yiyuanh/snare/internal/diff"
	"github.com/yiyuanh/snare/internal/lang"
	"github.com/yiyuanh/snare/internal/runner"
	"github.com/yiyuanh/snare/internal/testgen"
	"github.com/yiyuanh/snare/pkg/model"
)

// Options configures the pipeline.
type Options struct {
	Dir      string
	Staged   bool
	Commit   string
	Model    string
	MaxTests int
	Verbose  bool
	DryRun   bool
	Timeout  time.Duration
	APIKey   string
	Bedrock  bool
}

// Pipeline orchestrates the 5-stage JiT test generation process.
type Pipeline struct {
	opts Options
}

// New creates a new pipeline with the given options.
func New(opts Options) *Pipeline {
	return &Pipeline{opts: opts}
}

// Run executes the full pipeline.
func (p *Pipeline) Run(ctx context.Context) (*model.PipelineResult, error) {
	start := time.Now()
	result := &model.PipelineResult{}

	// Resolve working directory to absolute path
	dir, err := filepath.Abs(p.opts.Dir)
	if err != nil {
		return nil, fmt.Errorf("resolving directory: %w", err)
	}

	// Find module root (directory containing go.mod)
	moduleDir, err := findModuleRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("finding module root: %w", err)
	}

	// Stage 1: Diff Extraction
	if p.opts.Verbose {
		fmt.Println("Stage 1: Extracting diffs...")
	}
	extractor := diff.NewExtractor(moduleDir)
	fileDiffs, err := extractor.Extract(p.opts.Staged, p.opts.Commit)
	if err != nil {
		return nil, fmt.Errorf("extracting diffs: %w", err)
	}
	if len(fileDiffs) == 0 {
		fmt.Println("No Go file changes detected.")
		result.Duration = time.Since(start)
		return result, nil
	}
	result.FilesAnalyzed = len(fileDiffs)

	if p.opts.Verbose {
		for _, fd := range fileDiffs {
			fmt.Printf("  %s (%d hunks)\n", fd.NewName, len(fd.Hunks))
		}
	}

	// Stage 2: AST Analysis
	if p.opts.Verbose {
		fmt.Println("Stage 2: Analyzing changed functions...")
	}
	changedFuncs, err := analysis.MapChangedFuncs(fileDiffs)
	if err != nil {
		return nil, fmt.Errorf("analyzing changes: %w", err)
	}
	if len(changedFuncs) == 0 {
		fmt.Println("No changed functions detected in Go files.")
		result.Duration = time.Since(start)
		return result, nil
	}
	result.FuncsAnalyzed = len(changedFuncs)

	if p.opts.Verbose {
		for _, fn := range changedFuncs {
			fmt.Printf("  %s.%s (lines %d-%d)\n", fn.Package, fn.Name, fn.StartLine, fn.EndLine)
		}
	}

	// Stage 3: LLM Generation
	if p.opts.Verbose {
		fmt.Println("Stage 3: Generating mutants and tests via Claude...")
	}
	goLang := lang.NewGo()
	gen := testgen.NewGenerator(ctx, p.opts.Model, goLang, p.opts.MaxTests, p.opts.Verbose, p.opts.Bedrock)

	type genResult struct {
		fn      model.ChangedFunc
		mutants []model.Mutant
		tests   []model.GeneratedTest
	}
	var generated []genResult

	for _, fn := range changedFuncs {
		if p.opts.Verbose {
			fmt.Printf("  Generating for %s...\n", fn.Name)
		}
		mutants, tests, err := gen.Generate(ctx, fn)
		if err != nil {
			fmt.Printf("  Warning: generation failed for %s: %v\n", fn.Name, err)
			continue
		}
		result.MutantsGenerated += len(mutants)
		result.TestsGenerated += len(tests)
		generated = append(generated, genResult{fn: fn, mutants: mutants, tests: tests})

		if p.opts.Verbose {
			fmt.Printf("  Generated %d mutants, %d tests for %s\n", len(mutants), len(tests), fn.Name)
		}
	}

	if len(generated) == 0 {
		fmt.Println("No tests were generated.")
		result.Duration = time.Since(start)
		return result, nil
	}

	// Stage 4: Test Execution (skip if dry-run)
	if p.opts.DryRun {
		if p.opts.Verbose {
			fmt.Println("Stage 4: Skipped (dry-run mode)")
		}
		// Populate results without execution
		for _, g := range generated {
			mutantMap := make(map[string]model.Mutant)
			for _, m := range g.mutants {
				mutantMap[m.ID] = m
			}
			for _, t := range g.tests {
				result.Results = append(result.Results, model.TestResult{
					Test:   t,
					Mutant: mutantMap[t.MutantID],
				})
			}
		}
		result.Duration = time.Since(start)
		return result, nil
	}

	if p.opts.Verbose {
		fmt.Println("Stage 4: Executing tests...")
	}
	executor := runner.NewExecutor(moduleDir, goLang, p.opts.Timeout, p.opts.Verbose)

	for _, g := range generated {
		// Read the original source file
		originalSource, err := os.ReadFile(g.fn.FilePath)
		if err != nil {
			fmt.Printf("  Warning: cannot read %s: %v\n", g.fn.FilePath, err)
			continue
		}

		mutantMap := make(map[string]model.Mutant)
		for _, m := range g.mutants {
			mutantMap[m.ID] = m
		}

		for _, t := range g.tests {
			mutant, ok := mutantMap[t.MutantID]
			if !ok {
				fmt.Printf("  Warning: test %s references unknown mutant %s\n", t.TestName, t.MutantID)
				continue
			}

			result.TestsRun++
			tr, err := executor.ExecuteWithFile(t, mutant, g.fn.FilePath, originalSource)
			if err != nil {
				fmt.Printf("  Warning: execution failed for %s: %v\n", t.TestName, err)
				tr.FilteredReason = fmt.Sprintf("execution error: %v", err)
			}
			result.Results = append(result.Results, tr)
		}
	}

	// Stage 5: Assessment
	if p.opts.Verbose {
		fmt.Println("Stage 5: Assessing results...")
	}
	chain := assess.DefaultChain()
	result.Results = chain.Evaluate(result.Results)

	// Count catching and filtered
	for _, r := range result.Results {
		if r.IsCatching {
			result.CatchingTests++
		}
		if r.FilteredReason != "" {
			result.FilteredTests++
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

// findModuleRoot walks up from dir until it finds a go.mod file.
func findModuleRoot(dir string) (string, error) {
	current := dir
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return dir, fmt.Errorf("no go.mod found (searched from %s to root)", dir)
		}
		current = parent
	}
}
