package testgen

import (
	"strings"
	"testing"

	"github.com/yiyuanh/snare/pkg/model"
)

func TestBuildPrompt(t *testing.T) {
	fn := model.ChangedFunc{
		FilePath:    "pkg/math/add.go",
		Package:     "math",
		Name:        "Add",
		Signature:   "func Add(a, b int) int",
		Body:        "{\n\treturn a + b\n}",
		StartLine:   5,
		EndLine:     7,
		Imports:     []string{`"fmt"`},
		TypeDefs:    []string{"type Number int"},
		DiffContext: "+\treturn a + b",
	}

	prompt := BuildPrompt(fn)

	checks := []struct {
		name    string
		substr  string
	}{
		{"package name", "Package: math"},
		{"file path", "File: pkg/math/add.go"},
		{"function body", "return a + b"},
		{"diff context", "+\treturn a + b"},
		{"JSON format", `"mutants"`},
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
