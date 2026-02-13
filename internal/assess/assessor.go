package assess

import "github.com/yiyuanh/snare/pkg/model"

// Assessor evaluates test results and adjusts confidence or filters them.
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

// DefaultChain returns the standard assessment chain.
func DefaultChain() *Chain {
	return NewChain(
		&CompilationFilter{},
		&CatchingAssessor{},
		&TrivialMutantFilter{},
		&ErrorMessageFilter{},
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
		for _, a := range c.assessors {
			a.Assess(&results[i])
		}
	}
	return results
}
