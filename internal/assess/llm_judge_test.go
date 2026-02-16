package assess

import (
	"strings"
	"testing"

	"github.com/yiyuanh/snare/pkg/model"
)

func TestBuildJudgePrompt_ContainsTestCode(t *testing.T) {
	result := &model.TestResult{
		Test: model.GeneratedTest{
			TestName: "TestFoo_BoundaryCheck",
			TestCode: `package foo

import "testing"

func TestFoo_BoundaryCheck(t *testing.T) {
	result := Foo(0)
	if result != "zero" {
		t.Errorf("got %q, want %q", result, "zero")
	}
}`,
		},
		Mutant: model.Mutant{
			Description: "Boundary condition may be off by one",
		},
		ParentOutput: "ok  \tpkg/foo\t0.001s",
		DiffOutput:   "--- FAIL: TestFoo_BoundaryCheck\n    got \"negative\", want \"zero\"",
	}

	prompt := BuildJudgePrompt(result)

	checks := []struct {
		name   string
		substr string
	}{
		{"test code", "TestFoo_BoundaryCheck"},
		{"parent output", "ok"},
		{"diff output", "FAIL"},
		{"risk description", "Boundary condition"},
		{"assessment instruction", "unexpected bug"},
		{"json format", `"assessment"`},
		{"behavior change field", `"behavior_change"`},
	}

	for _, c := range checks {
		if !strings.Contains(prompt, c.substr) {
			t.Errorf("judge prompt missing %s: expected to contain %q", c.name, c.substr)
		}
	}
}

func TestBuildJudgePrompt_TruncatesLongOutput(t *testing.T) {
	longOutput := strings.Repeat("x", 1000)
	result := &model.TestResult{
		Test:         model.GeneratedTest{TestCode: "package foo"},
		Mutant:       model.Mutant{Description: "test risk"},
		ParentOutput: longOutput,
		DiffOutput:   longOutput,
	}

	prompt := BuildJudgePrompt(result)

	// Should contain truncation marker
	if !strings.Contains(prompt, "truncated") {
		t.Error("judge prompt should truncate long output")
	}
}
