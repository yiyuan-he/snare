package assess

import (
	"testing"

	"github.com/yiyuanh/snare/pkg/model"
)

func TestChain_CompilationFailure(t *testing.T) {
	results := []model.TestResult{
		{
			PassParent:   true,
			FailDiff:     true,
			ParentOutput: "[build failed]",
		},
	}

	chain := DefaultRuleOnlyChain()
	evaluated := chain.Evaluate(results)

	r := evaluated[0]
	if r.FilteredReason != "compilation failure" {
		t.Errorf("FilteredReason = %q, want %q", r.FilteredReason, "compilation failure")
	}
	if r.IsCatching {
		t.Error("IsCatching should be false for compilation failure")
	}
}

func TestChain_FailsOnParent(t *testing.T) {
	results := []model.TestResult{
		{
			PassParent: false,
			FailDiff:   true,
		},
	}

	chain := DefaultRuleOnlyChain()
	evaluated := chain.Evaluate(results)

	r := evaluated[0]
	if r.FilteredReason != "fails on parent code" {
		t.Errorf("FilteredReason = %q, want %q", r.FilteredReason, "fails on parent code")
	}
	if r.IsCatching {
		t.Error("IsCatching should be false")
	}
}

func TestChain_Catching(t *testing.T) {
	results := []model.TestResult{
		{
			PassParent: true,
			FailDiff:   true,
			DiffOutput: "--- FAIL: TestFoo\n    got 5, expected 10\n",
			Mutant: model.Mutant{
				Original: "x > 0",
				Mutated:  "x >= 0",
			},
		},
	}

	chain := DefaultRuleOnlyChain()
	evaluated := chain.Evaluate(results)

	r := evaluated[0]
	if !r.IsCatching {
		t.Error("IsCatching should be true")
	}
	if r.FilteredReason != "" {
		t.Errorf("FilteredReason = %q, want empty", r.FilteredReason)
	}
	// Assessment should be positive (weak catch starting at 0.5, boosted by true positive patterns)
	if r.Assessment <= 0 {
		t.Errorf("Assessment = %f, want > 0", r.Assessment)
	}
}

func TestChain_NoCatch(t *testing.T) {
	results := []model.TestResult{
		{
			PassParent: true,
			FailDiff:   false,
		},
	}

	chain := DefaultRuleOnlyChain()
	evaluated := chain.Evaluate(results)

	r := evaluated[0]
	if r.IsCatching {
		t.Error("IsCatching should be false when test passes on both parent and new code")
	}
	if r.Assessment != 0 {
		t.Errorf("Assessment = %f, want 0 for no catch", r.Assessment)
	}
}

func TestFalsePositivePatterns_Reflection(t *testing.T) {
	results := []model.TestResult{
		{
			PassParent: true,
			FailDiff:   true,
			DiffOutput: "FAIL",
			Test: model.GeneratedTest{
				TestCode: `package foo
import "reflect"
func TestFoo(t *testing.T) { reflect.DeepEqual(a, b) }`,
			},
			Mutant: model.Mutant{Original: "x > 0", Mutated: "x >= 0"},
		},
	}

	chain := DefaultRuleOnlyChain()
	evaluated := chain.Evaluate(results)

	r := evaluated[0]
	if !r.IsCatching {
		t.Error("IsCatching should be true")
	}
	// Assessment should be reduced due to reflection usage
	if r.Assessment >= 0.5 {
		t.Errorf("Assessment = %f, should be reduced for reflection usage", r.Assessment)
	}
}

func TestTruePositivePatterns_BoolChange(t *testing.T) {
	results := []model.TestResult{
		{
			PassParent: true,
			FailDiff:   true,
			DiffOutput: "--- FAIL: TestFoo\n    got: true, want: false\n",
			Mutant:     model.Mutant{Original: "x > 0", Mutated: "x >= 0"},
		},
	}

	chain := DefaultRuleOnlyChain()
	evaluated := chain.Evaluate(results)

	r := evaluated[0]
	if !r.IsCatching {
		t.Error("IsCatching should be true")
	}
	// Assessment should be boosted for bool change
	if r.Assessment <= 0.5 {
		t.Errorf("Assessment = %f, should be boosted for boolean change", r.Assessment)
	}
}

func TestFalsePositivePatterns_UndefinedVariable(t *testing.T) {
	results := []model.TestResult{
		{
			PassParent: true,
			FailDiff:   true,
			DiffOutput: "undefined: someVar\n",
			Mutant:     model.Mutant{Original: "x > 0", Mutated: "x >= 0"},
		},
	}

	chain := DefaultRuleOnlyChain()
	evaluated := chain.Evaluate(results)

	r := evaluated[0]
	if r.IsCatching {
		t.Error("IsCatching should be false for undefined variable (false positive)")
	}
	if r.FilteredReason != "undefined variable (likely rename)" {
		t.Errorf("FilteredReason = %q, want %q", r.FilteredReason, "undefined variable (likely rename)")
	}
}
