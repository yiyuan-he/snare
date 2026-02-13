package assess

import (
	"testing"

	"github.com/yiyuanh/snare/pkg/model"
)

func TestChain_CompilationFailure(t *testing.T) {
	results := []model.TestResult{
		{
			PassOriginal:   true,
			FailMutant:     true,
			OriginalOutput: "[build failed]",
		},
	}

	chain := DefaultChain()
	evaluated := chain.Evaluate(results)

	r := evaluated[0]
	if r.FilteredReason != "compilation failure" {
		t.Errorf("FilteredReason = %q, want %q", r.FilteredReason, "compilation failure")
	}
	if r.IsCatching {
		t.Error("IsCatching should be false for compilation failure")
	}
}

func TestChain_FailsOnOriginal(t *testing.T) {
	results := []model.TestResult{
		{
			PassOriginal: false,
			FailMutant:   true,
		},
	}

	chain := DefaultChain()
	evaluated := chain.Evaluate(results)

	r := evaluated[0]
	if r.FilteredReason != "fails on original code" {
		t.Errorf("FilteredReason = %q, want %q", r.FilteredReason, "fails on original code")
	}
	if r.IsCatching {
		t.Error("IsCatching should be false")
	}
}

func TestChain_Catching(t *testing.T) {
	results := []model.TestResult{
		{
			PassOriginal: true,
			FailMutant:   true,
			Mutant: model.Mutant{
				Original: "x > 0",
				Mutated:  "x >= 0",
			},
		},
	}

	chain := DefaultChain()
	evaluated := chain.Evaluate(results)

	r := evaluated[0]
	if !r.IsCatching {
		t.Error("IsCatching should be true")
	}
	if r.FilteredReason != "" {
		t.Errorf("FilteredReason = %q, want empty", r.FilteredReason)
	}
	if r.Confidence != 1.0 {
		t.Errorf("Confidence = %f, want 1.0", r.Confidence)
	}
}

func TestChain_TrivialMutant(t *testing.T) {
	results := []model.TestResult{
		{
			PassOriginal: true,
			FailMutant:   true,
			Mutant: model.Mutant{
				Original: "x",  // len < 3
				Mutated:  "y",  // len < 3
			},
		},
	}

	chain := DefaultChain()
	evaluated := chain.Evaluate(results)

	r := evaluated[0]
	if !r.IsCatching {
		t.Error("IsCatching should be true")
	}
	if r.Confidence >= 1.0 {
		t.Errorf("Confidence = %f, should be reduced for trivial mutant", r.Confidence)
	}
}

func TestChain_ClearAssertionBoost(t *testing.T) {
	results := []model.TestResult{
		{
			PassOriginal: true,
			FailMutant:   true,
			MutantOutput: "--- FAIL: TestFoo\n    got 5, expected 10\n",
			Mutant: model.Mutant{
				Original: "x > 10",
				Mutated:  "x > 20",
			},
		},
	}

	chain := DefaultChain()
	evaluated := chain.Evaluate(results)

	r := evaluated[0]
	if !r.IsCatching {
		t.Error("IsCatching should be true")
	}
	// Confidence should be boosted but capped at 1.0
	if r.Confidence > 1.0 {
		t.Errorf("Confidence = %f, should be capped at 1.0", r.Confidence)
	}
}
