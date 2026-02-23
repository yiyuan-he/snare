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
	flagOutput    string
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
	runCmd.Flags().StringVar(&flagOutput, "output", "", "Write detailed markdown report to file")
	rootCmd.AddCommand(runCmd)
}

// formatTokens formats a token count with comma separators.
func formatTokens(n int64) string {
	s := fmt.Sprintf("%d", n)
	if n < 0 {
		return s
	}
	// Insert commas from the right
	out := make([]byte, 0, len(s)+(len(s)-1)/3)
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(c))
	}
	return string(out)
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

	if flagOutput != "" {
		if err := writeMarkdownReport(result, opts, flagOutput); err != nil {
			return fmt.Errorf("writing report to %s: %w", flagOutput, err)
		}
		fmt.Printf("\n  Report written to %s\n", flagOutput)
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
				Mutant:           r.Mutant,
				Risk:             model.Risk{ID: r.Mutant.RiskID, Description: r.Mutant.Description},
				TelemetryContext: r.TelemetryContext,
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

// TelemetrySummary holds parsed telemetry fields for structured display.
type TelemetrySummary struct {
	CallVolume string
	Endpoints  string
	Callers    string
	Exceptions string
	Incidents  string
}

// ParseTelemetryContext extracts structured fields from the telemetry context string.
func ParseTelemetryContext(ctx string) TelemetrySummary {
	var s TelemetrySummary
	for _, line := range strings.Split(ctx, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "This function is called "):
			s.CallVolume = strings.TrimPrefix(line, "This function is called ")
			s.CallVolume = strings.TrimSuffix(s.CallVolume, ".")
		case strings.HasPrefix(line, "Triggered by endpoints: "):
			s.Endpoints = strings.TrimPrefix(line, "Triggered by endpoints: ")
		case strings.HasPrefix(line, "Called by: "):
			s.Callers = strings.TrimPrefix(line, "Called by: ")
		case strings.HasPrefix(line, "Known exceptions: "):
			s.Exceptions = strings.TrimPrefix(line, "Known exceptions: ")
		case strings.HasPrefix(line, "Recent incidents: "):
			s.Incidents = strings.TrimPrefix(line, "Recent incidents: ")
		}
	}
	return s
}

// FuncGroup holds all findings for a single function.
type FuncGroup struct {
	FuncName         string
	TelemetryContext string
	LikelyBugs      []model.CatchSummary
	WeakCatches     []model.CatchSummary
	NoCatch         []model.CatchSummary
}

// groupByFunction groups CatchSummary entries by function name and partitions
// each group into likely bugs, weak catches, and no-catch categories.
func groupByFunction(summaries []model.CatchSummary) []FuncGroup {
	order := []string{}
	groups := map[string]*FuncGroup{}

	for _, s := range summaries {
		fn := s.Mutant.FuncName
		g, ok := groups[fn]
		if !ok {
			g = &FuncGroup{
				FuncName:         fn,
				TelemetryContext: s.TelemetryContext,
			}
			groups[fn] = g
			order = append(order, fn)
		}
		if g.TelemetryContext == "" && s.TelemetryContext != "" {
			g.TelemetryContext = s.TelemetryContext
		}

		if s.IsWeakCatch && s.Assessment > 0.5 {
			g.LikelyBugs = append(g.LikelyBugs, s)
		} else if s.IsWeakCatch {
			g.WeakCatches = append(g.WeakCatches, s)
		} else {
			g.NoCatch = append(g.NoCatch, s)
		}
	}

	result := make([]FuncGroup, 0, len(order))
	for _, fn := range order {
		g := groups[fn]
		sort.Slice(g.LikelyBugs, func(i, j int) bool {
			return g.LikelyBugs[i].Assessment > g.LikelyBugs[j].Assessment
		})
		result = append(result, *g)
	}

	// Sort functions: highest assessment first, likely bugs before weak-only
	sort.SliceStable(result, func(i, j int) bool {
		iHasBugs := len(result[i].LikelyBugs) > 0
		jHasBugs := len(result[j].LikelyBugs) > 0
		if iHasBugs != jHasBugs {
			return iHasBugs
		}
		return funcGroupMaxAssessment(result[i]) > funcGroupMaxAssessment(result[j])
	})

	return result
}

func funcGroupMaxAssessment(g FuncGroup) float64 {
	max := 0.0
	for _, s := range g.LikelyBugs {
		if s.Assessment > max {
			max = s.Assessment
		}
	}
	for _, s := range g.WeakCatches {
		if s.Assessment > max {
			max = s.Assessment
		}
	}
	return max
}

func printReport(result *model.PipelineResult, opts pipeline.Options) {
	summaries := aggregateByCatch(result.Results)

	fmt.Println()
	fmt.Println(color.Apply(color.Bold, "═══════════════════════════════════════════════"))
	fmt.Println(color.Apply(color.Bold, "  snare — JIT Catching Report"))
	fmt.Println(color.Apply(color.Bold, "═══════════════════════════════════════════════"))
	fmt.Println()

	if opts.DryRun {
		fmt.Printf("  Files analyzed: %d | Functions: %d | Risks: %d | Tests: %d generated\n",
			result.FilesAnalyzed, result.FuncsAnalyzed, result.RisksIdentified, result.TestsGenerated)
		fmt.Printf("  Duration: %s\n", result.Duration.Round(time.Millisecond))
		fmt.Println()
		printDryRunReport(summaries, opts)
		return
	}

	// Compact summary line
	fmt.Printf("  Files analyzed: %d | Functions: %d | Risks: %d | Tests: %d/%d executed\n",
		result.FilesAnalyzed, result.FuncsAnalyzed, result.RisksIdentified,
		result.TestsRun, result.TestsGenerated)
	fmt.Printf("  Likely bugs: %s | Weak catches: %s | Duration: %s\n",
		color.Apply(color.Red+color.Bold, fmt.Sprintf("%d", result.StrongCatches)),
		color.Apply(color.Yellow, fmt.Sprintf("%d", result.WeakCatches)),
		result.Duration.Round(time.Millisecond))

	totalIn := result.GenerationUsage.InputTokens + result.AssessmentUsage.InputTokens
	totalOut := result.GenerationUsage.OutputTokens + result.AssessmentUsage.OutputTokens
	if totalIn > 0 || totalOut > 0 {
		fmt.Printf("  Tokens: %s in / %s out (gen: %s in / %s out, judge: %s in / %s out)\n",
			formatTokens(totalIn), formatTokens(totalOut),
			formatTokens(result.GenerationUsage.InputTokens), formatTokens(result.GenerationUsage.OutputTokens),
			formatTokens(result.AssessmentUsage.InputTokens), formatTokens(result.AssessmentUsage.OutputTokens))
	}
	fmt.Println()

	groups := groupByFunction(summaries)
	for _, g := range groups {
		printFuncGroup(g, opts)
	}
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

func printFuncGroup(g FuncGroup, opts pipeline.Options) {
	// Function header with trailing dashes
	header := fmt.Sprintf("── %s ", g.FuncName)
	if pad := 48 - len(header); pad > 0 {
		header += strings.Repeat("─", pad)
	}
	fmt.Println(color.Apply(color.Bold, header))

	// Telemetry line
	if g.TelemetryContext != "" {
		ts := ParseTelemetryContext(g.TelemetryContext)
		if ts.CallVolume != "" {
			prod := "Production: " + ts.CallVolume
			if ts.Endpoints != "" {
				prod += " via " + ts.Endpoints
			}
			fmt.Printf("  %s\n", color.Apply(color.Dim, prod))
		}
		var extra []string
		if ts.Exceptions != "" {
			extra = append(extra, "Exceptions: "+ts.Exceptions)
		}
		if ts.Incidents != "" {
			extra = append(extra, "Incidents: "+ts.Incidents)
		}
		if len(extra) > 0 {
			fmt.Printf("  %s\n", color.Apply(color.Dim, strings.Join(extra, " | ")))
		}
	}

	// Function with only no-catch entries
	if len(g.LikelyBugs) == 0 && len(g.WeakCatches) == 0 {
		totalTests := 0
		for _, s := range g.NoCatch {
			totalTests += len(s.Tests)
		}
		fmt.Println()
		fmt.Printf("  No behavioral changes detected. (%d tests passed on both versions)\n", totalTests)
		fmt.Println()
		return
	}

	fmt.Println()

	// Likely bugs with question on next line
	for _, s := range g.LikelyBugs {
		label := color.Apply(color.Red+color.Bold, fmt.Sprintf("LIKELY BUG (%.2f)", s.Assessment))
		fmt.Printf("  %s %s\n", label, s.Mutant.Description)
		if s.Question != "" {
			for _, line := range wrapText(s.Question, 74) {
				fmt.Printf("    %s\n", color.Apply(color.Dim, "> "+line))
			}
		}

		if opts.Verbose {
			for _, t := range s.Tests {
				if t.IsCatching {
					fmt.Printf("    Test: %s (assessment: %.2f)\n", t.Test.TestName, t.Assessment)
					fmt.Println()
					fmt.Println(t.Test.TestCode)
					fmt.Println()
				}
			}
		}
		fmt.Println()
	}

	// Weak catches as compact one-liners
	for _, s := range g.WeakCatches {
		label := color.Apply(color.Yellow, fmt.Sprintf("weak catch (%.2f)", s.Assessment))
		fmt.Printf("  %s %s\n", label, s.Mutant.Description)
	}
	if len(g.WeakCatches) > 0 {
		fmt.Println()
	}
}

// wrapText breaks a string into lines of at most maxWidth characters at word boundaries.
func wrapText(s string, maxWidth int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	current := words[0]
	for _, w := range words[1:] {
		if len(current)+1+len(w) > maxWidth {
			lines = append(lines, current)
			current = w
		} else {
			current += " " + w
		}
	}
	return append(lines, current)
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

	header := fmt.Sprintf("── Filtered (%d) ", len(filtered))
	if pad := 48 - len(header); pad > 0 {
		header += strings.Repeat("─", pad)
	}
	fmt.Println(color.Apply(color.Dim, header))
	fmt.Printf("  %s\n", color.Apply(color.Dim, fmt.Sprintf("%d tests filtered (fails on parent code)", len(filtered))))
	fmt.Println()
}

// writeMarkdownReport writes a detailed markdown report to the given file path.
func writeMarkdownReport(result *model.PipelineResult, opts pipeline.Options, outputPath string) error {
	var sb strings.Builder

	sb.WriteString("# snare — JIT Catching Report\n\n")

	// Summary table
	sb.WriteString("| Metric | Value |\n")
	sb.WriteString("|--------|-------|\n")
	sb.WriteString(fmt.Sprintf("| Files analyzed | %d |\n", result.FilesAnalyzed))
	sb.WriteString(fmt.Sprintf("| Functions analyzed | %d |\n", result.FuncsAnalyzed))
	sb.WriteString(fmt.Sprintf("| Risks identified | %d |\n", result.RisksIdentified))
	sb.WriteString(fmt.Sprintf("| Tests generated | %d |\n", result.TestsGenerated))
	sb.WriteString(fmt.Sprintf("| Tests executed | %d |\n", result.TestsRun))
	sb.WriteString(fmt.Sprintf("| Likely bugs | %d |\n", result.StrongCatches))
	sb.WriteString(fmt.Sprintf("| Weak catches | %d |\n", result.WeakCatches))
	sb.WriteString(fmt.Sprintf("| Duration | %s |\n", result.Duration.Round(time.Millisecond)))
	totalIn := result.GenerationUsage.InputTokens + result.AssessmentUsage.InputTokens
	totalOut := result.GenerationUsage.OutputTokens + result.AssessmentUsage.OutputTokens
	if totalIn > 0 || totalOut > 0 {
		sb.WriteString(fmt.Sprintf("| Generation tokens | %s in / %s out |\n",
			formatTokens(result.GenerationUsage.InputTokens), formatTokens(result.GenerationUsage.OutputTokens)))
		sb.WriteString(fmt.Sprintf("| Assessment tokens | %s in / %s out |\n",
			formatTokens(result.AssessmentUsage.InputTokens), formatTokens(result.AssessmentUsage.OutputTokens)))
		sb.WriteString(fmt.Sprintf("| Total tokens | %s in / %s out |\n",
			formatTokens(totalIn), formatTokens(totalOut)))
	}
	sb.WriteString("\n")

	summaries := aggregateByCatch(result.Results)
	groups := groupByFunction(summaries)

	for i, g := range groups {
		if i > 0 {
			sb.WriteString("---\n\n")
		}
		sb.WriteString(fmt.Sprintf("## %s\n\n", g.FuncName))

		// Telemetry section
		if g.TelemetryContext != "" {
			ts := ParseTelemetryContext(g.TelemetryContext)
			sb.WriteString("### Production Telemetry\n")
			if ts.CallVolume != "" {
				sb.WriteString(fmt.Sprintf("- **Call volume:** %s\n", ts.CallVolume))
			}
			if ts.Endpoints != "" {
				sb.WriteString(fmt.Sprintf("- **Endpoints:** %s\n", ts.Endpoints))
			}
			if ts.Callers != "" {
				sb.WriteString(fmt.Sprintf("- **Callers:** %s\n", ts.Callers))
			}
			if ts.Exceptions != "" {
				sb.WriteString(fmt.Sprintf("- **Exceptions:** %s\n", ts.Exceptions))
			} else {
				sb.WriteString("- **Exceptions:** none\n")
			}
			if ts.Incidents != "" {
				sb.WriteString(fmt.Sprintf("- **Incidents:** %s\n", ts.Incidents))
			} else {
				sb.WriteString("- **Incidents:** none\n")
			}
			sb.WriteString("\n")
		}

		// Likely bugs
		if len(g.LikelyBugs) > 0 {
			sb.WriteString("### Likely Bugs\n\n")
			for _, s := range g.LikelyBugs {
				sb.WriteString(fmt.Sprintf("#### %s (assessment: %.2f)\n\n", s.Mutant.Description, s.Assessment))
				sb.WriteString(fmt.Sprintf("**Risk:** %s\n\n", s.Risk.Description))
				if s.BehaviorChange != "" {
					sb.WriteString(fmt.Sprintf("**Behavioral change:** %s\n\n", s.BehaviorChange))
				}
				if s.Question != "" {
					sb.WriteString(fmt.Sprintf("> %s\n\n", s.Question))
				}
				for _, t := range s.Tests {
					if t.IsCatching && t.Test.TestCode != "" {
						sb.WriteString("<details>\n")
						sb.WriteString(fmt.Sprintf("<summary>Test: %s</summary>\n\n", t.Test.TestName))
						sb.WriteString("```\n")
						sb.WriteString(t.Test.TestCode)
						if !strings.HasSuffix(t.Test.TestCode, "\n") {
							sb.WriteString("\n")
						}
						sb.WriteString("```\n\n")
						sb.WriteString("</details>\n\n")
					}
				}
			}
		}

		// Weak catches
		if len(g.WeakCatches) > 0 {
			sb.WriteString("### Weak Catches\n\n")
			for _, s := range g.WeakCatches {
				sb.WriteString(fmt.Sprintf("- **%s** (assessment: %.2f)", s.Mutant.Description, s.Assessment))
				if s.Question != "" {
					sb.WriteString(fmt.Sprintf(" — %s", s.Question))
				}
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}

		// No-catch-only function
		if len(g.LikelyBugs) == 0 && len(g.WeakCatches) == 0 && len(g.NoCatch) > 0 {
			totalTests := 0
			for _, s := range g.NoCatch {
				totalTests += len(s.Tests)
			}
			sb.WriteString(fmt.Sprintf("No behavioral changes detected. (%d tests passed on both versions)\n\n", totalTests))
		}
	}

	// Summary table
	sb.WriteString("---\n\n")
	sb.WriteString("## Summary\n\n")
	sb.WriteString("| Function | Likely Bugs | Weak Catches | No Catch |\n")
	sb.WriteString("|----------|-------------|--------------|----------|\n")
	for _, g := range groups {
		sb.WriteString(fmt.Sprintf("| %s | %d | %d | %d |\n",
			g.FuncName, len(g.LikelyBugs), len(g.WeakCatches), len(g.NoCatch)))
	}
	sb.WriteString("\n")

	return os.WriteFile(outputPath, []byte(sb.String()), 0644)
}
