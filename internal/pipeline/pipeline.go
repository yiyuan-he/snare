package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yiyuanh/snare/internal/analysis"
	"github.com/yiyuanh/snare/internal/assess"
	"github.com/yiyuanh/snare/internal/diff"
	"github.com/yiyuanh/snare/internal/lang"
	"github.com/yiyuanh/snare/internal/runner"
	"github.com/yiyuanh/snare/internal/telemetry"
	"github.com/yiyuanh/snare/internal/testgen"
	"github.com/yiyuanh/snare/pkg/model"
)

// Options configures the pipeline.
type Options struct {
	Dir           string
	Staged        bool
	Commit        string
	Model         string
	MaxTests      int
	Verbose       bool
	DryRun        bool
	Timeout       time.Duration
	APIKey        string
	Bedrock       bool
	CommitMessage string // populated during pipeline run
	TelemetryDB   string // path to telemetry SQLite database
}

// Pipeline orchestrates the 5-stage JiT catching test process.
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

	// Find project root (directory containing go.mod or setup.py/pyproject.toml)
	moduleDir, err := findProjectRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("finding project root: %w", err)
	}

	// Stage 1: Diff Extraction (with parent source retrieval)
	if p.opts.Verbose {
		fmt.Println("Stage 1: Extracting diffs and parent sources...")
	}
	extractor := diff.NewExtractor(moduleDir)
	fileDiffs, err := extractor.Extract(p.opts.Staged, p.opts.Commit)
	if err != nil {
		return nil, fmt.Errorf("extracting diffs: %w", err)
	}
	if len(fileDiffs) == 0 {
		fmt.Println("No source file changes detected.")
		result.Duration = time.Since(start)
		return result, nil
	}
	result.FilesAnalyzed = len(fileDiffs)

	// Fetch commit message for context
	commitMsg, err := extractor.GetCommitMessage(p.opts.Commit)
	if err != nil {
		if p.opts.Verbose {
			fmt.Printf("  Warning: could not get commit message: %v\n", err)
		}
	}
	p.opts.CommitMessage = commitMsg

	if p.opts.Verbose {
		for _, fd := range fileDiffs {
			hasParent := "no parent"
			if len(fd.ParentSource) > 0 {
				hasParent = "with parent"
			}
			fmt.Printf("  %s (%d hunks, %s)\n", fd.NewName, len(fd.Hunks), hasParent)
		}
	}

	// Detect language from file diffs
	language := detectLanguage(fileDiffs)
	if p.opts.Verbose {
		fmt.Printf("  Detected language: %s\n", language.Name())
	}

	// Stage 2: AST Analysis (dual-version: parent + new)
	if p.opts.Verbose {
		fmt.Println("Stage 2: Analyzing changed functions (parent + new)...")
	}

	var changedFuncs []model.ChangedFunc
	if language.Name() == "go" {
		// Use Go-specific AST analysis (backward compatible)
		changedFuncs, err = analysis.MapChangedFuncs(fileDiffs)
	} else {
		// Use language-agnostic analysis via Language interface
		changedFuncs, err = analysis.MapChangedFuncsWithLang(fileDiffs, language)
	}
	if err != nil {
		return nil, fmt.Errorf("analyzing changes: %w", err)
	}
	if len(changedFuncs) == 0 {
		fmt.Printf("No changed functions detected in %s files.\n", language.Name())
		result.Duration = time.Since(start)
		return result, nil
	}
	result.FuncsAnalyzed = len(changedFuncs)

	// Enrich with telemetry data if available
	if p.opts.TelemetryDB != "" {
		if p.opts.Verbose {
			fmt.Printf("  Loading telemetry from %s...\n", p.opts.TelemetryDB)
		}
		if err := p.enrichWithTelemetry(changedFuncs); err != nil {
			if p.opts.Verbose {
				fmt.Printf("  Warning: telemetry enrichment failed: %v\n", err)
			}
		}
	}

	if p.opts.Verbose {
		for _, fn := range changedFuncs {
			hasParent := "new function"
			if fn.ParentBody != "" {
				hasParent = "modified"
			}
			telemetryInfo := ""
			if fn.TelemetryContext != "" {
				telemetryInfo = ", with telemetry"
			}
			fmt.Printf("  %s.%s (lines %d-%d, %s%s)\n", fn.Package, fn.Name, fn.StartLine, fn.EndLine, hasParent, telemetryInfo)
		}
	}

	// Stage 3: Intent-Aware Generation
	if p.opts.Verbose {
		fmt.Println("Stage 3: Generating intent-aware catching tests via Claude...")
	}
	gen := testgen.NewGenerator(ctx, p.opts.Model, language, p.opts.MaxTests, p.opts.Verbose, p.opts.Bedrock)

	type genResult struct {
		fn      model.ChangedFunc
		intent  string
		risks   []model.Risk
		mutants []model.Mutant
		tests   []model.GeneratedTest
	}
	var generated []genResult

	for _, fn := range changedFuncs {
		if p.opts.Verbose {
			fmt.Printf("  Generating for %s...\n", fn.Name)
		}
		intent, risks, mutants, tests, err := gen.Generate(ctx, fn, p.opts.CommitMessage)
		if err != nil {
			fmt.Printf("  Warning: generation failed for %s: %v\n", fn.Name, err)
			continue
		}
		result.MutantsGenerated += len(mutants)
		result.TestsGenerated += len(tests)
		result.RisksIdentified += len(risks)
		generated = append(generated, genResult{fn: fn, intent: intent, risks: risks, mutants: mutants, tests: tests})

		if p.opts.Verbose {
			fmt.Printf("  Intent: %s\n", intent)
			fmt.Printf("  Generated %d risks, %d mutants, %d tests for %s\n", len(risks), len(mutants), len(tests), fn.Name)
		}
	}

	if len(generated) == 0 {
		fmt.Println("No tests were generated.")
		result.Duration = time.Since(start)
		return result, nil
	}

	// Aggregate intent for reporting
	var intents []string
	for _, g := range generated {
		if g.intent != "" {
			intents = append(intents, g.intent)
		}
	}
	if len(intents) > 0 {
		result.Intent = intents[0]
	}

	// Build file diff lookup for parent source
	fileDiffMap := make(map[string]model.FileDiff)
	for _, fd := range fileDiffs {
		fileDiffMap[fd.NewName] = fd
	}

	// Stage 4: Catching Execution (skip if dry-run)
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
				tr := model.TestResult{
					Test:   t,
					Mutant: mutantMap[t.MutantID],
				}
				if g.fn.TelemetryContext != "" {
					tr.TelemetryContext = g.fn.TelemetryContext
				}
				result.Results = append(result.Results, tr)
			}
		}
		result.Duration = time.Since(start)
		return result, nil
	}

	if p.opts.Verbose {
		fmt.Println("Stage 4: Executing catching tests (parent vs new)...")
	}
	executor := runner.NewExecutor(moduleDir, language, p.opts.Timeout, p.opts.Verbose)

	for _, g := range generated {
		// Get parent source from the file diff
		fd, ok := fileDiffMap[g.fn.FilePath]
		if !ok || len(fd.ParentSource) == 0 {
			fmt.Printf("  Warning: no parent source for %s, skipping catching execution\n", g.fn.FilePath)
			continue
		}

		// Get new source: from the commit (if available) or from disk
		var newSource []byte
		if len(fd.NewSource) > 0 {
			newSource = fd.NewSource
		} else {
			var err error
			newSource, err = os.ReadFile(g.fn.FilePath)
			if err != nil {
				fmt.Printf("  Warning: cannot read %s: %v\n", g.fn.FilePath, err)
				continue
			}
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
			tr, err := executor.ExecuteCatching(t, mutant, g.fn.FilePath, fd.ParentSource, newSource)
			if err != nil {
				fmt.Printf("  Warning: execution failed for %s: %v\n", t.TestName, err)
				tr.FilteredReason = fmt.Sprintf("execution error: %v", err)
			}
			// Pass through telemetry context for the judge
			if g.fn.TelemetryContext != "" {
				tr.TelemetryContext = g.fn.TelemetryContext
			}
			result.Results = append(result.Results, tr)
		}
	}

	// Stage 5: Assessment (rule-based patterns + LLM-as-judge on weak catches)
	if p.opts.Verbose {
		fmt.Println("Stage 5: Assessing results (rule-based + LLM judge)...")
	}
	chain := assess.DefaultCatchingChain(gen.Client(), p.opts.Model, ctx, p.opts.Verbose, p.opts.CommitMessage)
	result.Results = chain.Evaluate(result.Results)

	// Count weak/strong catches and filtered
	for _, r := range result.Results {
		if r.IsCatching {
			result.WeakCatches++
			if r.Assessment > 0.5 {
				result.StrongCatches++
			}
		}
		if r.FilteredReason != "" {
			result.FilteredTests++
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

// detectLanguage examines file diffs to determine the project language.
// Returns Python if any .py files are present, otherwise Go.
func detectLanguage(fileDiffs []model.FileDiff) lang.Language {
	for _, fd := range fileDiffs {
		name := fd.NewName
		if name == "" {
			name = fd.OldName
		}
		if strings.HasSuffix(name, ".py") {
			return lang.NewPython()
		}
	}
	return lang.NewGo()
}

// enrichWithTelemetry opens the telemetry database and enriches changed functions
// with production telemetry context.
func (p *Pipeline) enrichWithTelemetry(changedFuncs []model.ChangedFunc) error {
	reader, err := telemetry.NewReader(p.opts.TelemetryDB)
	if err != nil {
		return fmt.Errorf("opening telemetry DB: %w", err)
	}
	defer reader.Close()

	for i := range changedFuncs {
		ft, err := reader.GetFunctionTelemetry(changedFuncs[i].Name, changedFuncs[i].FilePath)
		if err != nil {
			if p.opts.Verbose {
				fmt.Printf("  Warning: telemetry lookup failed for %s: %v\n", changedFuncs[i].Name, err)
			}
			continue
		}
		if ft != nil {
			changedFuncs[i].TelemetryContext = telemetry.FormatForPrompt(ft)
		}
	}
	return nil
}

// findProjectRoot walks up from dir until it finds a project root marker.
// Supports go.mod (Go), setup.py, pyproject.toml, or requirements.txt (Python).
func findProjectRoot(dir string) (string, error) {
	markers := []string{"go.mod", "setup.py", "pyproject.toml", "requirements.txt"}
	current := dir
	for {
		for _, marker := range markers {
			if _, err := os.Stat(filepath.Join(current, marker)); err == nil {
				return current, nil
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return dir, fmt.Errorf("no project root found (searched from %s to root)", dir)
		}
		current = parent
	}
}
