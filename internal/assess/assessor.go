package assess

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/yiyuanh/snare/pkg/model"
)

// Assessor evaluates test results and adjusts assessment or filters them.
type Assessor interface {
	Assess(result *model.TestResult)
}

// Chain runs a sequence of assessors on each test result.
type Chain struct {
	assessors []Assessor
}

// NewChain creates an assessment chain from the given assessors.
func NewChain(assessors ...Assessor) *Chain {
	return &Chain{assessors: assessors}
}

// DefaultCatchingChain returns the assessment chain for the catching workflow.
// It includes rule-based pattern matching and optionally an LLM judge.
func DefaultCatchingChain(client *anthropic.Client, modelID string, ctx context.Context, verbose bool) *Chain {
	assessors := []Assessor{
		&CompilationFilter{},
		&CatchingAssessor{},
		&FalsePositivePatterns{},
		&TruePositivePatterns{},
	}

	if client != nil {
		assessors = append(assessors, NewLLMJudge(client, modelID, ctx, verbose))
	}

	return NewChain(assessors...)
}

// DefaultRuleOnlyChain returns an assessment chain with only rule-based assessors.
// Useful for dry-run mode or when no LLM client is available.
func DefaultRuleOnlyChain() *Chain {
	return NewChain(
		&CompilationFilter{},
		&CatchingAssessor{},
		&FalsePositivePatterns{},
		&TruePositivePatterns{},
	)
}

// Evaluate runs all assessors on each result.
func (c *Chain) Evaluate(results []model.TestResult) []model.TestResult {
	for i := range results {
		if results[i].FilteredReason != "" {
			// Already filtered during execution
			continue
		}
		results[i].Confidence = 1.0
		results[i].Assessment = 0
		for _, a := range c.assessors {
			a.Assess(&results[i])
		}
	}
	return results
}
