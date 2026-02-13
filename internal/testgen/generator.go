package testgen

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	bedrockpkg "github.com/anthropics/anthropic-sdk-go/bedrock"
	"github.com/yiyuanh/snare/internal/lang"
	"github.com/yiyuanh/snare/pkg/model"
)

// Generator uses Claude to generate mutants and tests.
type Generator struct {
	client   *anthropic.Client
	model    string
	lang     lang.Language
	maxTests int
	verbose  bool
}

// NewGenerator creates a new LLM-based test generator.
// When bedrock is true, the client uses AWS credentials via the default config chain
// instead of ANTHROPIC_API_KEY.
func NewGenerator(ctx context.Context, modelID string, language lang.Language, maxTests int, verbose bool, bedrock bool) *Generator {
	var client anthropic.Client
	if bedrock {
		client = anthropic.NewClient(bedrockpkg.WithLoadDefaultConfig(ctx))
	} else {
		client = anthropic.NewClient()
	}
	return &Generator{
		client:   &client,
		model:    modelID,
		lang:     language,
		maxTests: maxTests,
		verbose:  verbose,
	}
}

// Generate produces mutants and tests for a changed function.
// It makes a single API call per function and includes one retry on parse failure.
func (g *Generator) Generate(ctx context.Context, fn model.ChangedFunc) ([]model.Mutant, []model.GeneratedTest, error) {
	prompt := BuildPrompt(fn)

	mutants, tests, err := g.callAndParse(ctx, prompt, fn)
	if err != nil {
		// One retry with error context
		retryPrompt := prompt + "\n\n## Previous attempt failed\nError: " + err.Error() + "\nPlease fix the issue and try again. Remember to output ONLY valid JSON."
		mutants, tests, err = g.callAndParse(ctx, retryPrompt, fn)
		if err != nil {
			return nil, nil, fmt.Errorf("generation failed after retry: %w", err)
		}
	}

	// Apply max-tests limit
	if g.maxTests > 0 && len(tests) > g.maxTests {
		tests = tests[:g.maxTests]
		// Also trim mutants to only those referenced by kept tests
		kept := make(map[string]bool)
		for _, t := range tests {
			kept[t.MutantID] = true
		}
		var filteredMutants []model.Mutant
		for _, m := range mutants {
			if kept[m.ID] {
				filteredMutants = append(filteredMutants, m)
			}
		}
		mutants = filteredMutants
	}

	return mutants, tests, nil
}

func (g *Generator) callAndParse(ctx context.Context, prompt string, fn model.ChangedFunc) ([]model.Mutant, []model.GeneratedTest, error) {
	resp, err := g.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(g.model),
		MaxTokens: 4096,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("Claude API call: %w", err)
	}

	// Extract text from response
	var text string
	for _, block := range resp.Content {
		if block.Type == "text" {
			text = block.Text
			break
		}
	}

	if text == "" {
		return nil, nil, fmt.Errorf("empty response from Claude")
	}

	// Strip markdown code fences if present
	text = stripCodeFences(text)

	// Parse JSON response
	var llmResp model.LLMResponse
	if err := json.Unmarshal([]byte(text), &llmResp); err != nil {
		return nil, nil, fmt.Errorf("parsing LLM response JSON: %w (response: %.500s)", err, text)
	}

	if len(llmResp.Mutants) == 0 {
		return nil, nil, fmt.Errorf("no mutants generated")
	}
	if len(llmResp.Tests) == 0 {
		return nil, nil, fmt.Errorf("no tests generated")
	}

	// Set func name on all mutants and tests
	for i := range llmResp.Mutants {
		llmResp.Mutants[i].FuncName = fn.Name
	}
	for i := range llmResp.Tests {
		llmResp.Tests[i].FuncName = fn.Name
	}

	// Validate test syntax
	for i, t := range llmResp.Tests {
		if err := g.lang.ValidateTestSyntax([]byte(t.TestCode)); err != nil {
			return nil, nil, fmt.Errorf("test %s has syntax error: %w\ncode:\n%s", t.TestName, err, t.TestCode)
		}
		_ = i
	}

	return llmResp.Mutants, llmResp.Tests, nil
}

func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
	}
	if strings.HasSuffix(s, "```") {
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}
