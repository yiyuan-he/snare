package testgen

import (
	"strings"
	"testing"

	"github.com/yiyuanh/snare/pkg/model"
)

func TestBuildCatchingPrompt(t *testing.T) {
	fn := model.ChangedFunc{
		FilePath:        "pkg/math/add.go",
		Package:         "math",
		Name:            "Add",
		Signature:       "func Add(a, b int) int",
		Body:            "{\n\treturn a + b\n}",
		StartLine:       5,
		EndLine:         7,
		Imports:         []string{`"fmt"`},
		TypeDefs:        []string{"type Number int"},
		DiffContext:     "+\treturn a + b",
		ParentSignature: "func Add(a, b int) int",
		ParentBody:      "{\n\treturn a - b\n}",
	}

	prompt := BuildCatchingPrompt(fn)

	checks := []struct {
		name   string
		substr string
	}{
		{"package name", "Package: math"},
		{"file path", "File: pkg/math/add.go"},
		{"parent function header", "Parent (OLD) Function"},
		{"parent body", "return a - b"},
		{"new function header", "Current (NEW) Function"},
		{"new body", "return a + b"},
		{"diff context", "+\treturn a + b"},
		{"intent instruction", "Infer Intent"},
		{"risks instruction", "Identify Risks"},
		{"risk mutants instruction", "Generate Risk Mutants"},
		{"catching tests instruction", "Generate Catching Tests"},
		{"JSON format intent", `"intent"`},
		{"JSON format risks", `"risks"`},
		{"JSON format risk_id", `"risk_id"`},
		{"imports", `"fmt"`},
		{"type defs", "type Number int"},
		{"package in tests", `must be "math"`},
	}

	for _, c := range checks {
		if !strings.Contains(prompt, c.substr) {
			t.Errorf("prompt missing %s: expected to contain %q", c.name, c.substr)
		}
	}
}

func TestBuildCatchingPrompt_WithCommitMessage(t *testing.T) {
	fn := model.ChangedFunc{
		FilePath:        "pkg/math/add.go",
		Package:         "math",
		Name:            "Add",
		Signature:       "func Add(a, b int) int",
		Body:            "{\n\treturn a + b\n}",
		DiffContext:     "+\treturn a + b",
		ParentSignature: "func Add(a, b int) int",
		ParentBody:      "{\n\treturn a - b\n}",
	}

	prompt := BuildCatchingPrompt(fn, "fix: correct addition logic")

	if !strings.Contains(prompt, "Commit Context") {
		t.Error("prompt should contain commit context section")
	}
	if !strings.Contains(prompt, "fix: correct addition logic") {
		t.Error("prompt should contain the commit message")
	}
}

func TestBuildCatchingPrompt_WithoutCommitMessage(t *testing.T) {
	fn := model.ChangedFunc{
		FilePath:    "pkg/math/add.go",
		Package:     "math",
		Name:        "Add",
		Signature:   "func Add(a, b int) int",
		Body:        "{\n\treturn a + b\n}",
		DiffContext: "+\treturn a + b",
	}

	prompt := BuildCatchingPrompt(fn)

	if strings.Contains(prompt, "Commit Context") {
		t.Error("prompt should not contain commit context section when no message provided")
	}
}

func TestBuildCatchingPrompt_NoParent(t *testing.T) {
	// When no parent is available, prompt should use current code as fallback
	fn := model.ChangedFunc{
		FilePath:    "pkg/math/add.go",
		Package:     "math",
		Name:        "Add",
		Signature:   "func Add(a, b int) int",
		Body:        "{\n\treturn a + b\n}",
		DiffContext: "+\treturn a + b",
	}

	prompt := BuildCatchingPrompt(fn)

	// Parent section should contain current code as fallback
	if !strings.Contains(prompt, "Parent (OLD) Function") {
		t.Error("prompt missing parent function section")
	}
	// Both sections should have the same function body
	count := strings.Count(prompt, "return a + b")
	if count < 2 {
		t.Errorf("expected function body to appear at least twice (parent + new), got %d", count)
	}
}
