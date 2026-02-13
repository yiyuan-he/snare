package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yiyuanh/snare/internal/pipeline"
	"github.com/yiyuanh/snare/pkg/model"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run JiT tests on current changes",
	Long:  `Analyzes git diffs, generates mutation-based tests using Claude, and identifies catching tests.`,
	RunE:  runJiT,
}

var (
	flagStaged   bool
	flagCommit   string
	flagDir      string
	flagModel    string
	flagMaxTests int
	flagVerbose  bool
	flagDryRun   bool
	flagTimeout  time.Duration
	flagBedrock  bool
)

func init() {
	runCmd.Flags().BoolVar(&flagStaged, "staged", false, "Only analyze staged changes")
	runCmd.Flags().StringVar(&flagCommit, "commit", "", "Analyze changes from a specific commit")
	runCmd.Flags().StringVar(&flagDir, "dir", ".", "Working directory (defaults to current)")
	runCmd.Flags().StringVar(&flagModel, "model", "claude-sonnet-4-5-20250929", "Claude model to use")
	runCmd.Flags().IntVar(&flagMaxTests, "max-tests", 0, "Maximum number of tests to generate (0 = unlimited)")
	runCmd.Flags().BoolVarP(&flagVerbose, "verbose", "v", false, "Enable verbose output")
	runCmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "Generate tests but don't execute them")
	runCmd.Flags().DurationVar(&flagTimeout, "timeout", 30*time.Second, "Timeout for each test execution")
	runCmd.Flags().BoolVar(&flagBedrock, "bedrock", false, "Use Amazon Bedrock instead of the Anthropic API")
	rootCmd.AddCommand(runCmd)
}

func runJiT(cmd *cobra.Command, args []string) error {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" && !flagBedrock {
		return fmt.Errorf("ANTHROPIC_API_KEY environment variable is required (or use --bedrock)")
	}

	opts := pipeline.Options{
		Dir:      flagDir,
		Staged:   flagStaged,
		Commit:   flagCommit,
		Model:    flagModel,
		MaxTests: flagMaxTests,
		Verbose:  flagVerbose,
		DryRun:   flagDryRun,
		Timeout:  flagTimeout,
		APIKey:   apiKey,
		Bedrock:  flagBedrock,
	}

	p := pipeline.New(opts)
	result, err := p.Run(cmd.Context())
	if err != nil {
		return err
	}

	printReport(result, opts)
	return nil
}

// aggregateByMutant groups test results by FuncName:MutantID composite key.
func aggregateByMutant(results []model.TestResult) []model.MutantSummary {
	type key struct{ funcName, mutantID string }
	order := []key{}
	groups := map[key]*model.MutantSummary{}

	for _, r := range results {
		k := key{r.Mutant.FuncName, r.Mutant.ID}
		s, ok := groups[k]
		if !ok {
			s = &model.MutantSummary{Mutant: r.Mutant}
			groups[k] = s
			order = append(order, k)
		}
		s.Tests = append(s.Tests, r)
		if r.IsCatching {
			s.IsCaught = true
		}
		if r.Confidence > s.BestConfidence {
			s.BestConfidence = r.Confidence
		}
	}

	out := make([]model.MutantSummary, 0, len(order))
	for _, k := range order {
		out = append(out, *groups[k])
	}
	return out
}

// partitionMutants splits summaries into uncaught and caught slices.
func partitionMutants(summaries []model.MutantSummary) (uncaught, caught []model.MutantSummary) {
	for _, s := range summaries {
		if s.IsCaught {
			caught = append(caught, s)
		} else {
			uncaught = append(uncaught, s)
		}
	}
	return
}

func printReport(result *model.PipelineResult, opts pipeline.Options) {
	summaries := aggregateByMutant(result.Results)

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════")
	fmt.Println("  snare — Mutation Testing Report")
	fmt.Println("═══════════════════════════════════════════════")
	fmt.Println()

	if !opts.DryRun {
		uncaught, caught := partitionMutants(summaries)
		total := len(uncaught) + len(caught)
		pct := 0
		if total > 0 {
			pct = len(caught) * 100 / total
		}
		fmt.Printf("  Mutation coverage:  %d/%d caught (%d%%)\n", len(caught), total, pct)
		fmt.Println("  ──────────────────────────────────")
	}

	fmt.Printf("  Files analyzed:     %d\n", result.FilesAnalyzed)
	fmt.Printf("  Functions analyzed: %d\n", result.FuncsAnalyzed)
	fmt.Printf("  Mutants generated:  %d\n", result.MutantsGenerated)
	fmt.Printf("  Tests generated:    %d\n", result.TestsGenerated)

	if !opts.DryRun {
		fmt.Printf("  Tests executed:     %d\n", result.TestsRun)
	}

	fmt.Printf("  Duration:           %s\n", result.Duration.Round(time.Millisecond))
	fmt.Println()

	if opts.DryRun {
		printDryRunReport(summaries, opts)
		return
	}

	uncaught, caught := partitionMutants(summaries)
	printUncaughtSection(uncaught, opts)
	printCaughtSection(caught, opts)
	printFilteredSection(result.Results)
}

func printDryRunReport(summaries []model.MutantSummary, opts pipeline.Options) {
	fmt.Println("  [dry-run] Tests were generated but not executed.")
	fmt.Println()

	for i, s := range summaries {
		fmt.Printf("  %d. [%s] %s\n", i+1, s.Mutant.FuncName, s.Mutant.Description)
		fmt.Printf("     - original:  %s\n", strings.TrimSpace(s.Mutant.Original))
		fmt.Printf("     + mutated:   %s\n", strings.TrimSpace(s.Mutant.Mutated))

		if len(s.Tests) > 0 {
			fmt.Printf("     Tests (%d):\n", len(s.Tests))
			for _, t := range s.Tests {
				fmt.Printf("       - %s\n", t.Test.TestName)
				if opts.Verbose {
					fmt.Println()
					fmt.Println(t.Test.TestCode)
					fmt.Println()
				}
			}
		}
		fmt.Println()
	}
}

func printUncaughtSection(uncaught []model.MutantSummary, opts pipeline.Options) {
	fmt.Printf("── UNCAUGHT MUTATIONS (%d) ─────────────────────\n", len(uncaught))
	fmt.Println()

	if len(uncaught) == 0 {
		fmt.Println("  All mutations were caught!")
		fmt.Println()
		return
	}

	fmt.Println("  These mutations could be introduced without any test catching them.")
	fmt.Println()

	for i, s := range uncaught {
		fmt.Printf("  %d. [%s] %s\n", i+1, s.Mutant.FuncName, s.Mutant.Description)
		fmt.Printf("     - original:  %s\n", strings.TrimSpace(s.Mutant.Original))
		fmt.Printf("     + mutated:   %s\n", strings.TrimSpace(s.Mutant.Mutated))

		if opts.Verbose && len(s.Tests) > 0 {
			fmt.Printf("     Attempted tests (%d):\n", len(s.Tests))
			for _, t := range s.Tests {
				reason := "did not catch mutant"
				if t.FilteredReason != "" {
					reason = t.FilteredReason
				} else if !t.PassOriginal {
					reason = "fails on original code"
				}
				fmt.Printf("       - %s: %s\n", t.Test.TestName, reason)
			}
		}
		fmt.Println()
	}
}

func printCaughtSection(caught []model.MutantSummary, opts pipeline.Options) {
	fmt.Printf("── CAUGHT MUTATIONS (%d) ───────────────────────\n", len(caught))
	fmt.Println()

	if len(caught) == 0 {
		fmt.Println("  No mutations were caught.")
		fmt.Println()
		return
	}

	for i, s := range caught {
		fmt.Printf("  %d. [%s] %s (%.0f%% confidence)\n", i+1, s.Mutant.FuncName, s.Mutant.Description, s.BestConfidence*100)

		if opts.Verbose {
			for _, t := range s.Tests {
				if t.IsCatching {
					fmt.Printf("     ✓ %s (%.0f%%)\n", t.Test.TestName, t.Confidence*100)
					fmt.Println()
					fmt.Println(t.Test.TestCode)
					fmt.Println()
				}
			}
		}
	}
	fmt.Println()
}

func printFilteredSection(results []model.TestResult) {
	var filtered []model.TestResult
	for _, r := range results {
		if r.FilteredReason != "" {
			filtered = append(filtered, r)
		}
	}

	if len(filtered) == 0 {
		return
	}

	fmt.Printf("── Filtered Tests (%d) ─────────────────────────\n", len(filtered))
	for _, r := range filtered {
		fmt.Printf("  %s: %s\n", r.Test.TestName, r.FilteredReason)
	}
	fmt.Println()
}
