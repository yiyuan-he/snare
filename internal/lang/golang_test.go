package lang

import (
	"strings"
	"testing"
)

func TestApplyMutant_Success(t *testing.T) {
	src := []byte(`package example

func Add(a, b int) int {
	return a + b
}
`)

	g := NewGo()
	result, err := g.ApplyMutant(src, "a + b", "a - b")
	if err != nil {
		t.Fatalf("ApplyMutant returned error: %v", err)
	}

	if !strings.Contains(string(result), "a - b") {
		t.Error("result does not contain mutated code")
	}
	if strings.Contains(string(result), "a + b") {
		t.Error("result still contains original code")
	}
}

func TestApplyMutant_SnippetNotFound(t *testing.T) {
	src := []byte(`package example

func Add(a, b int) int {
	return a + b
}
`)

	g := NewGo()
	_, err := g.ApplyMutant(src, "nonexistent snippet", "replacement")
	if err == nil {
		t.Fatal("expected error for missing snippet, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want it to contain 'not found'", err.Error())
	}
}

func TestApplyMutant_InvalidGoAfterReplacement(t *testing.T) {
	src := []byte(`package example

func Add(a, b int) int {
	return a + b
}
`)

	g := NewGo()
	// Replace with syntactically invalid Go
	_, err := g.ApplyMutant(src, "return a + b", "return {{{invalid")
	if err == nil {
		t.Fatal("expected error for invalid Go after mutation, got nil")
	}
	if !strings.Contains(err.Error(), "not valid Go") {
		t.Errorf("error = %q, want it to contain 'not valid Go'", err.Error())
	}
}

func TestValidateTestSyntax_Valid(t *testing.T) {
	code := []byte(`package example

import "testing"

func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Error("expected 3")
	}
}
`)

	g := NewGo()
	if err := g.ValidateTestSyntax(code); err != nil {
		t.Errorf("ValidateTestSyntax returned error for valid code: %v", err)
	}
}

func TestValidateTestSyntax_Invalid(t *testing.T) {
	code := []byte(`package example

func TestBroken(t *testing.T) {
	if {{{
}
`)

	g := NewGo()
	if err := g.ValidateTestSyntax(code); err == nil {
		t.Error("expected error for invalid syntax, got nil")
	}
}
