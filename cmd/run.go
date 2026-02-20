package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yiyuanh/snare/internal/color"
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
	flagStaged    bool
	flagCommit    string
	flagDir       string
	flagModel     string
	flagMaxTests  int
	flagVerbose   bool
	flagDryRun    bool
	flagTimeout   time.Duration
	flagBedrock   bool
	flagJSON      bool
	flagFormat    string
	flagTelemetry string
)

func init() {
	runCmd.Flags().BoolVar(&flagStaged, "staged", false, "Only analyze staged changes")
	runCmd.Flags().StringVar(&flagCommit, "commit", "", "Analyze changes from a specific commit")
	runCmd.Flags().StringVar(&flagDir, "dir", ".", "Working directory (defaults to current)")
	runCmd.Flags().StringVar(&flagModel, "model", "us.anthropic.claude-opus-4-6-v1", "Claude model to use")
	runCmd.Flags().IntVar(&flagMaxTests, "max-tests", 0, "Maximum number of tests to generate (0 = unlimited)")
	runCmd.Flags().BoolVarP(&flagVerbose, "verbose", "v", false, "Enable verbose output")
	runCmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "Generate tests but don't execute them")
	runCmd.Flags().DurationVar(&flagTimeout, "timeout", 30*time.Second, "Timeout for each test execution")
	runCmd.Flags().BoolVar(&flagBedrock, "bedrock", false, "Use Amazon Bedrock instead of the Anthropic API")
	runCmd.Flags().BoolVar(&flagJSON, "json", false, "Output results as JSON")
	runCmd.Flags().StringVar(&flagFormat, "format", "text", "Output format: text, json, github")
	runCmd.Flags().StringVar(&flagTelemetry, "telemetry", "", "Path to telemetry SQLite database for enriched analysis")
	rootCmd.AddCommand(runCmd)
}

// outputFormat returns the resolved output format.
func outputFormat() string {
	if flagJSON {
		return "json"
	}
	return flagFormat
}

func runJiT(cmd *cobra.Command, args []string) error {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" && !flagBedrock {
		return fmt.Errorf("ANTHROPIC_API_KEY environment variable is required (or use --bedrock)")
	}

	// Disable color for non-text formats
	format := outputFormat()
	if format != "text" {
		color.SetEnabled(false)
	}

	opts := pipeline.Options{
		Dir:         flagDir,
		Staged:      flagStaged,
		Commit:      flagCommit,
		Model:       flagModel,
		MaxTests:    flagMaxTests,
		Verbose:     flagVerbose && format == "text",
		DryRun:      flagDryRun,
		Timeout:     flagTimeout,
		APIKey:      apiKey,
		Bedrock:     flagBedrock,
		TelemetryDB: flagTelemetry,
	}

	p := pipeline.New(opts)
	result, err := p.Run(cmd.Context())
	if err != nil {
		return err
	}

	switch format {
	case "json":
		return printJSON(result)
	case "github":
		printGitHub(result)
	default:
		printReport(result, opts)
	}
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
		if r.Question != "" {
			s.Question = r.Question
		}
	}

	out := make([]model.CatchSummary, 0, len(order))
	for _, k := range order {
		out = append(out, *groups[k])
	}
	return out
}

// printJSON outputs the full pipeline result as indented JSON.
func printJSON(result *model.PipelineResult) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

// printGitHub outputs a markdown report suitable for posting as a PR comment.
func printGitHub(result *model.PipelineResult) {
	summaries := aggregateByCatch(result.Results)

	var likelyBugs, weakCatches []model.CatchSummary
	for _, s := range summaries {
		if s.IsWeakCatch && s.Assessment > 0.5 {
			likelyBugs = append(likelyBugs, s)
		} else if s.IsWeakCatch {
			weakCatches = append(weakCatches, s)
		}
	}

	sort.Slice(likelyBugs, func(i, j int) bool {
		return likelyBugs[i].Assessment > likelyBugs[j].Assessment
	})

	fmt.Println("## snare — JIT Catching Report")
	fmt.Println()
	fmt.Printf("**Weak catches:** %d found | **Likely bugs:** %d\n", result.WeakCatches, result.StrongCatches)
	fmt.Println()

	if len(likelyBugs) > 0 {
		fmt.Println("### Likely Bugs")
		fmt.Println()
		for _, s := range likelyBugs {
			fmt.Printf("> **[%s] %s** (assessment: %.2f)\n", s.Mutant.FuncName, s.Mutant.Description, s.Assessment)
			fmt.Printf("> Risk: %s\n", s.Risk.Description)
			if s.Question != "" {
				fmt.Printf("> %s\n", s.Question)
			}
			fmt.Println()
		}
	}

	if len(weakCatches) > 0 {
		fmt.Println("<details>")
		fmt.Printf("<summary>Weak Catches (%d)</summary>\n", len(weakCatches))
		fmt.Println()
		for _, s := range weakCatches {
			question := ""
			if s.Question != "" {
				question = " — " + s.Question
			}
			fmt.Printf("- [%s] %s (%.2f)%s\n", s.Mutant.FuncName, s.Mutant.Description, s.Assessment, question)
		}
		fmt.Println()
		fmt.Println("</details>")
		fmt.Println()
	}

	fmt.Println("---")
	fmt.Println("*Generated by [snare](https://github.com/yiyuanh/snare)*")
}

func printReport(result *model.PipelineResult, opts pipeline.Options) {
	summaries := aggregateByCatch(result.Results)

	fmt.Println()
	fmt.Println(color.Apply(color.Bold, "═══════════════════════════════════════════════"))
	fmt.Println(color.Apply(color.Bold, "  snare — JIT Catching Report"))
	fmt.Println(color.Apply(color.Bold, "═══════════════════════════════════════════════"))
	fmt.Println()

	if !opts.DryRun {
		fmt.Printf("  Weak catches:     %s\n", color.Apply(color.Bold, fmt.Sprintf("%d found", result.WeakCatches)))
		fmt.Printf("  Likely bugs:      %s\n", color.Apply(color.Bold, fmt.Sprintf("%d (assessment > 0.5)", result.StrongCatches)))
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
	header := fmt.Sprintf("── LIKELY BUGS (%d) ────────────────────────────", len(likelyBugs))
	fmt.Println(color.Apply(color.Red+color.Bold, header))
	fmt.Println()

	if len(likelyBugs) == 0 {
		fmt.Println("  No likely bugs detected.")
		fmt.Println()
		return
	}

	fmt.Println("  These behavioral changes appear unintentional and may indicate bugs.")
	fmt.Println()

	for i, s := range likelyBugs {
		assessStr := fmt.Sprintf("%.2f", s.Assessment)
		if s.Assessment > 0.5 {
			assessStr = color.Apply(color.Red, assessStr)
		}
		fmt.Printf("  %d. [%s] %s (assessment: %s)\n", i+1, s.Mutant.FuncName, s.Mutant.Description, assessStr)
		fmt.Printf("     Risk: %s\n", s.Risk.Description)
		if s.BehaviorChange != "" {
			fmt.Printf("     Change: %s\n", s.BehaviorChange)
		}
		if s.Question != "" {
			fmt.Printf("     %s\n", color.Apply(color.Bold, "> "+s.Question))
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
	header := fmt.Sprintf("── WEAK CATCHES (%d) ───────────────────────────", len(weakCatches))
	fmt.Println(color.Apply(color.Yellow, header))
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
		if s.Question != "" {
			fmt.Printf("     %s\n", color.Apply(color.Bold, "> "+s.Question))
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
	header := fmt.Sprintf("── NO CATCH (%d) ───────────────────────────────", len(noCatch))
	fmt.Println(color.Apply(color.Green, header))
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

	header := fmt.Sprintf("── FILTERED (%d) ───────────────────────────────", len(filtered))
	fmt.Println(color.Apply(color.Dim, header))
	for _, r := range filtered {
		fmt.Printf("  %s\n", color.Apply(color.Dim, fmt.Sprintf("%s: %s", r.Test.TestName, r.FilteredReason)))
	}
	fmt.Println()
}
