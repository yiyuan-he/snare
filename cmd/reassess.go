package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	bedrockpkg "github.com/anthropics/anthropic-sdk-go/bedrock"
	"github.com/spf13/cobra"
	"github.com/yiyuanh/snare/internal/assess"
	"github.com/yiyuanh/snare/internal/color"
	"github.com/yiyuanh/snare/internal/pipeline"
	"github.com/yiyuanh/snare/pkg/model"
)

var reassessCmd = &cobra.Command{
	Use:   "reassess <results.json>",
	Short: "Re-run assessment on saved pipeline results",
	Long:  `Re-runs the assessment chain (rule-based + LLM judge) on a previously saved JSON pipeline result, allowing controlled experiments on the effect of telemetry context.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runReassess,
}

var flagNoTelemetry bool

func init() {
	reassessCmd.Flags().BoolVar(&flagNoTelemetry, "no-telemetry", false, "Strip telemetry context from all results before assessment")
	reassessCmd.Flags().StringVar(&flagModel, "model", "us.anthropic.claude-opus-4-6-v1", "Claude model for the LLM judge")
	reassessCmd.Flags().BoolVar(&flagBedrock, "bedrock", false, "Use Amazon Bedrock instead of the Anthropic API")
	reassessCmd.Flags().BoolVarP(&flagVerbose, "verbose", "v", false, "Enable verbose output")
	reassessCmd.Flags().StringVar(&flagFormat, "format", "text", "Output format: text, json, github")
	reassessCmd.Flags().StringVar(&flagOutput, "output", "", "Write detailed markdown report to file")
	reassessCmd.Flags().BoolVar(&flagJSON, "json", false, "Shorthand for --format json")
	rootCmd.AddCommand(reassessCmd)
}

func runReassess(cmd *cobra.Command, args []string) error {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" && !flagBedrock {
		return fmt.Errorf("ANTHROPIC_API_KEY environment variable is required (or use --bedrock)")
	}

	// Disable color for non-text formats
	format := outputFormat()
	if format != "text" {
		color.SetEnabled(false)
	}

	// Read and parse the JSON results file
	data, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("reading results file: %w", err)
	}

	var result model.PipelineResult
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("parsing results JSON: %w", err)
	}

	// Strip telemetry context if --no-telemetry
	if flagNoTelemetry {
		for i := range result.Results {
			result.Results[i].TelemetryContext = ""
		}
	}

	// Reset assessment fields on non-execution-filtered results so the chain
	// recomputes everything from scratch.
	for i := range result.Results {
		if strings.HasPrefix(result.Results[i].FilteredReason, "execution error:") {
			continue
		}
		result.Results[i].Assessment = 0
		result.Results[i].Confidence = 0
		result.Results[i].BehaviorChange = ""
		result.Results[i].Question = ""
		result.Results[i].IsCatching = false
		result.Results[i].FilteredReason = ""
	}

	// Create Anthropic client
	ctx := cmd.Context()
	var client anthropic.Client
	if flagBedrock {
		client = anthropic.NewClient(bedrockpkg.WithLoadDefaultConfig(ctx))
	} else {
		client = anthropic.NewClient()
	}

	// Build and run assessment chain
	verbose := flagVerbose && format == "text"
	chain := assess.DefaultCatchingChain(&client, flagModel, ctx, verbose, "")

	start := time.Now()
	result.Results = chain.Evaluate(result.Results)

	// Recount catches and filtered tests
	result.WeakCatches = 0
	result.StrongCatches = 0
	result.FilteredTests = 0
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

	// Update token usage: only assessment was re-run
	result.AssessmentUsage = chain.TokenUsage()
	result.GenerationUsage = model.TokenUsage{}
	result.Duration = time.Since(start)

	// Output using existing display helpers
	opts := pipeline.Options{
		Verbose: verbose,
	}

	switch format {
	case "json":
		return printJSON(&result)
	case "github":
		printGitHub(&result)
	default:
		printReport(&result, opts)
	}

	if flagOutput != "" {
		if err := writeMarkdownReport(&result, opts, flagOutput); err != nil {
			return fmt.Errorf("writing report to %s: %w", flagOutput, err)
		}
		fmt.Printf("\n  Report written to %s\n", flagOutput)
	}
	return nil
}
