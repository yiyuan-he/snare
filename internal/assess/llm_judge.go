package assess

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/yiyuanh/snare/pkg/model"
)

// LLMJudge uses Claude to assess whether a weak catch is a true or false positive.
type LLMJudge struct {
	client        *anthropic.Client
	model         string
	ctx           context.Context
	verbose       bool
	commitMessage string
}

// NewLLMJudge creates a new LLM-based assessor.
func NewLLMJudge(client *anthropic.Client, modelID string, ctx context.Context, verbose bool, commitMessage string) *LLMJudge {
	return &LLMJudge{
		client:        client,
		model:         modelID,
		ctx:           ctx,
		verbose:       verbose,
		commitMessage: commitMessage,
	}
}

type judgeResponse struct {
	Assessment     float64 `json:"assessment"`
	BehaviorChange string  `json:"behavior_change"`
	Question       string  `json:"question"`
}

func (j *LLMJudge) Assess(result *model.TestResult) {
	// Only assess weak catches (tests that pass on parent and fail on new code)
	if result.FilteredReason != "" || !result.IsCatching {
		return
	}

	prompt := buildJudgePrompt(result, j.commitMessage)

	resp, err := j.client.Messages.New(j.ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(j.model),
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		if j.verbose {
			fmt.Printf("  [judge] LLM assessment failed for %s: %v\n", result.Test.TestName, err)
		}
		return // Keep existing assessment on failure
	}

	var text string
	for _, block := range resp.Content {
		if block.Type == "text" {
			text = block.Text
			break
		}
	}

	text = strings.TrimSpace(text)
	// Strip code fences if present
	if strings.HasPrefix(text, "```json") {
		text = strings.TrimPrefix(text, "```json")
	} else if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```")
	}
	if strings.HasSuffix(text, "```") {
		text = strings.TrimSuffix(text, "```")
	}
	text = strings.TrimSpace(text)

	var jr judgeResponse
	if err := json.Unmarshal([]byte(text), &jr); err != nil {
		if j.verbose {
			fmt.Printf("  [judge] Failed to parse judge response for %s: %v\n", result.Test.TestName, err)
		}
		return
	}

	// Combine rule-based and LLM scores: weighted average (60% LLM, 40% rules)
	ruleScore := result.Assessment
	llmScore := jr.Assessment
	combined := ruleScore*0.4 + llmScore*0.6

	// Clamp to [-1, 1]
	if combined > 1 {
		combined = 1
	}
	if combined < -1 {
		combined = -1
	}

	result.Assessment = combined
	result.BehaviorChange = jr.BehaviorChange
	result.Question = jr.Question

	if j.verbose {
		fmt.Printf("  [judge] %s: rule=%.2f llm=%.2f combined=%.2f\n",
			result.Test.TestName, ruleScore, llmScore, combined)
	}
}

// BuildJudgePrompt constructs the prompt for the LLM judge. Exported for testing.
func BuildJudgePrompt(result *model.TestResult, commitMessage ...string) string {
	cm := ""
	if len(commitMessage) > 0 {
		cm = commitMessage[0]
	}
	return buildJudgePrompt(result, cm)
}

func buildJudgePrompt(result *model.TestResult, commitMessage string) string {
	var sb strings.Builder

	// Detect language from test code
	codeLang := "go"
	if strings.Contains(result.Test.TestCode, "import pytest") || strings.Contains(result.Test.TestCode, "def test_") {
		codeLang = "python"
	}

	sb.WriteString(`You are a code review expert assessing whether a test failure indicates a real bug or an expected behavior change.

## Test Code
` + "```" + codeLang + "\n" + result.Test.TestCode + "\n```\n\n")

	sb.WriteString("## Test passed on parent (old) code:\n```\n")
	// Truncate output to avoid excessive tokens
	parentOut := result.ParentOutput
	if len(parentOut) > 500 {
		parentOut = parentOut[:500] + "\n... (truncated)"
	}
	sb.WriteString(parentOut)
	sb.WriteString("\n```\n\n")

	sb.WriteString("## Test failed on new (changed) code:\n```\n")
	diffOut := result.DiffOutput
	if len(diffOut) > 500 {
		diffOut = diffOut[:500] + "\n... (truncated)"
	}
	sb.WriteString(diffOut)
	sb.WriteString("\n```\n\n")

	sb.WriteString("## Risk being tested\n")
	sb.WriteString(result.Mutant.Description)
	sb.WriteString("\n\n")

	if commitMessage != "" {
		sb.WriteString("## Commit Context\n")
		sb.WriteString(commitMessage)
		sb.WriteString("\n\n")
	}

	if result.TelemetryContext != "" {
		sb.WriteString("## Production Telemetry\n")
		sb.WriteString("The following production telemetry data provides context about how this function is used:\n\n")
		sb.WriteString(result.TelemetryContext)
		sb.WriteString("\n\n")
	}

	sb.WriteString(`## Task

Analyze whether this test failure represents:
- An **unexpected bug** introduced by the code change (positive score)
- An **expected/intentional** behavior change (negative score)
- Unclear/ambiguous (score near 0)

Respond with ONLY a JSON object:
{
  "assessment": <float from -1.0 to 1.0>,
  "behavior_change": "<one-sentence description of what behavioral change was detected>",
  "question": "<one-sentence question for the developer phrased as 'Is it expected that...' describing the specific behavioral change in concrete terms (values, types, states)>"
}

Guidelines:
- Score closer to 1.0: the failure clearly indicates an unintended bug
- Score closer to -1.0: the failure is an expected consequence of the intended change
- Score near 0: ambiguous, could go either way
- The "question" should help the developer quickly decide if the change is intentional
- If production telemetry is available, weigh it in your assessment:
  - High-traffic functions with behavioral changes are more likely to be bugs (score higher)
  - Functions with known exception patterns where the change addresses those exceptions may be intentional (score lower)
  - Consider whether the change aligns with or contradicts production usage patterns
`)

	return sb.String()
}
