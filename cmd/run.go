package cmd

import (
	"fmt"
	"os"
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
	rootCmd.AddCommand(runCmd)
}

func runJiT(cmd *cobra.Command, args []string) error {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY environment variable is required")
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
	}

	p := pipeline.New(opts)
	result, err := p.Run(cmd.Context())
	if err != nil {
		return err
	}

	printReport(result, opts)
	return nil
}

func printReport(result *model.PipelineResult, opts pipeline.Options) {
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════")
	fmt.Println("  snare — JiT Catching Test Report")
	fmt.Println("═══════════════════════════════════════════")
	fmt.Println()

	fmt.Printf("  Files analyzed:     %d\n", result.FilesAnalyzed)
	fmt.Printf("  Functions analyzed: %d\n", result.FuncsAnalyzed)
	fmt.Printf("  Mutants generated:  %d\n", result.MutantsGenerated)
	fmt.Printf("  Tests generated:    %d\n", result.TestsGenerated)

	if !opts.DryRun {
		fmt.Printf("  Tests executed:     %d\n", result.TestsRun)
		fmt.Printf("  Catching tests:     %d\n", result.CatchingTests)
		fmt.Printf("  Filtered tests:     %d\n", result.FilteredTests)
	}

	fmt.Printf("  Duration:           %s\n", result.Duration.Round(time.Millisecond))
	fmt.Println()

	if opts.DryRun {
		fmt.Println("  [dry-run] Tests were generated but not executed.")
		fmt.Println()
		for i, r := range result.Results {
			fmt.Printf("── Test %d: %s ──\n", i+1, r.Test.TestName)
			fmt.Printf("   Function: %s\n", r.Test.FuncName)
			fmt.Printf("   Mutation: %s\n", r.Mutant.Description)
			fmt.Println()
			fmt.Println(r.Test.TestCode)
			fmt.Println()
		}
		return
	}

	// Print catching tests
	catching := 0
	for _, r := range result.Results {
		if !r.IsCatching {
			continue
		}
		catching++
		fmt.Printf("── Catching Test %d: %s ──\n", catching, r.Test.TestName)
		fmt.Printf("   Function:   %s\n", r.Test.FuncName)
		fmt.Printf("   Mutation:   %s\n", r.Mutant.Description)
		fmt.Printf("   Confidence: %.0f%%\n", r.Confidence*100)
		fmt.Println()
		fmt.Println(r.Test.TestCode)
		fmt.Println()
	}

	if catching == 0 {
		fmt.Println("  No catching tests found.")
		fmt.Println()
	}

	// Print filtered tests summary
	filtered := 0
	for _, r := range result.Results {
		if r.FilteredReason != "" {
			filtered++
		}
	}
	if filtered > 0 {
		fmt.Printf("── Filtered Tests (%d) ──\n", filtered)
		for _, r := range result.Results {
			if r.FilteredReason != "" {
				fmt.Printf("   %s: %s\n", r.Test.TestName, r.FilteredReason)
			}
		}
		fmt.Println()
	}
}
