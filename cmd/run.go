package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yiyuanh/snare/internal/pipeline"
	"github.com/yiyuanh/snare/pkg/model"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run JiT catching tests on current changes",
	Long:  `Analyzes git diffs, infers intent, identifies risks, generates catching tests, and assesses behavioral changes.`,
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

// aggregateByCatch groups test results into CatchSummary entries by FuncName:MutantID.
func aggregateByCatch(results []model.TestResult) []model.CatchSummary {
	type key struct{ funcName, mutantID string }
	order := []key{}
	groups := map[key]*model.CatchSummary{}

	for _, r := range results {
		k := key{r.Mutant.FuncName, r.Mutant.ID}
		s, ok := groups[k]
		if !ok {
			s = &model.CatchSummary{
				Mutant: r.Mutant,
				Risk:   model.Risk{ID: r.Mutant.RiskID, Description: r.Mutant.Description},
			}
			groups[k] = s
			order = append(order, k)
		}
		s.Tests = append(s.Tests, r)
		if r.IsCatching {
			s.IsWeakCatch = true
		}
		if r.Assessment > s.Assessment {
			s.Assessment = r.Assessment
		}
		if r.BehaviorChange != "" {
			s.BehaviorChange = r.BehaviorChange
		}
	}

	out := make([]model.CatchSummary, 0, len(order))
	for _, k := range order {
		out = append(out, *groups[k])
	}
	return out
}

func printReport(result *model.PipelineResult, opts pipeline.Options) {
	summaries := aggregateByCatch(result.Results)

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════")
	fmt.Println("  snare — JIT Catching Report")
	fmt.Println("═══════════════════════════════════════════════")
	fmt.Println()

	if !opts.DryRun {
		fmt.Printf("  Weak catches:     %d found\n", result.WeakCatches)
		fmt.Printf("  Likely bugs:      %d (assessment > 0.5)\n", result.StrongCatches)
		fmt.Println("  ──────────────────────────────────")
	}

	fmt.Printf("  Files analyzed:     %d\n", result.FilesAnalyzed)
	fmt.Printf("  Functions analyzed: %d\n", result.FuncsAnalyzed)
	fmt.Printf("  Risks identified:   %d\n", result.RisksIdentified)
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

	// Partition summaries into likely bugs, weak catches, no catch
	var likelyBugs, weakCatches, noCatch []model.CatchSummary
	for _, s := range summaries {
		if s.IsWeakCatch && s.Assessment > 0.5 {
			likelyBugs = append(likelyBugs, s)
		} else if s.IsWeakCatch {
			weakCatches = append(weakCatches, s)
		} else {
			noCatch = append(noCatch, s)
		}
	}

	// Sort likely bugs by assessment (highest first)
	sort.Slice(likelyBugs, func(i, j int) bool {
		return likelyBugs[i].Assessment > likelyBugs[j].Assessment
	})

	printLikelyBugsSection(likelyBugs, opts)
	printWeakCatchesSection(weakCatches, opts)
	printNoCatchSection(noCatch)
	printFilteredSection(result.Results)
}

func printDryRunReport(summaries []model.CatchSummary, opts pipeline.Options) {
	fmt.Println("  [dry-run] Tests were generated but not executed.")
	fmt.Println()

	for i, s := range summaries {
		fmt.Printf("  %d. [%s] %s\n", i+1, s.Mutant.FuncName, s.Mutant.Description)
		fmt.Printf("     Risk: %s\n", s.Risk.Description)
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

func printLikelyBugsSection(likelyBugs []model.CatchSummary, opts pipeline.Options) {
	fmt.Printf("── LIKELY BUGS (%d) ────────────────────────────\n", len(likelyBugs))
	fmt.Println()

	if len(likelyBugs) == 0 {
		fmt.Println("  No likely bugs detected.")
		fmt.Println()
		return
	}

	fmt.Println("  These behavioral changes appear unintentional and may indicate bugs.")
	fmt.Println()

	for i, s := range likelyBugs {
		fmt.Printf("  %d. [%s] %s (assessment: %.2f)\n", i+1, s.Mutant.FuncName, s.Mutant.Description, s.Assessment)
		fmt.Printf("     Risk: %s\n", s.Risk.Description)
		if s.BehaviorChange != "" {
			fmt.Printf("     Change: %s\n", s.BehaviorChange)
		}

		if opts.Verbose {
			for _, t := range s.Tests {
				if t.IsCatching {
					fmt.Printf("     Test: %s (assessment: %.2f)\n", t.Test.TestName, t.Assessment)
					fmt.Println()
					fmt.Println(t.Test.TestCode)
					fmt.Println()
				}
			}
		}
		fmt.Println()
	}
}

func printWeakCatchesSection(weakCatches []model.CatchSummary, opts pipeline.Options) {
	fmt.Printf("── WEAK CATCHES (%d) ───────────────────────────\n", len(weakCatches))
	fmt.Println()

	if len(weakCatches) == 0 {
		fmt.Println("  No weak catches.")
		fmt.Println()
		return
	}

	fmt.Println("  These behavioral changes were detected but may be intentional.")
	fmt.Println()

	for i, s := range weakCatches {
		fmt.Printf("  %d. [%s] %s (assessment: %.2f)\n", i+1, s.Mutant.FuncName, s.Mutant.Description, s.Assessment)
		if s.BehaviorChange != "" {
			fmt.Printf("     Change: %s\n", s.BehaviorChange)
		}

		if opts.Verbose {
			for _, t := range s.Tests {
				if t.IsCatching {
					fmt.Printf("     Test: %s\n", t.Test.TestName)
				}
			}
		}
		fmt.Println()
	}
}

func printNoCatchSection(noCatch []model.CatchSummary) {
	fmt.Printf("── NO CATCH (%d) ───────────────────────────────\n", len(noCatch))
	fmt.Println()

	if len(noCatch) == 0 {
		fmt.Println("  All risks had behavioral changes detected.")
		fmt.Println()
		return
	}

	fmt.Println("  Tests for these risks passed on both old and new code (no behavioral change).")
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

	fmt.Printf("── FILTERED (%d) ───────────────────────────────\n", len(filtered))
	for _, r := range filtered {
		fmt.Printf("  %s: %s\n", r.Test.TestName, r.FilteredReason)
	}
	fmt.Println()
}
