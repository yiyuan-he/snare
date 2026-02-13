package testgen

import (
	"fmt"
	"strings"

	"github.com/yiyuanh/snare/pkg/model"
)

// BuildPrompt constructs the prompt for Claude to generate mutants and tests.
func BuildPrompt(fn model.ChangedFunc) string {
	var sb strings.Builder

	sb.WriteString("You are a mutation testing expert. Given a Go function and the diff showing recent changes, generate realistic mutants and catching tests.\n\n")

	sb.WriteString("## Context\n\n")
	sb.WriteString(fmt.Sprintf("Package: %s\n", fn.Package))
	sb.WriteString(fmt.Sprintf("File: %s\n\n", fn.FilePath))

	if len(fn.Imports) > 0 {
		sb.WriteString("### Imports\n```go\n")
		for _, imp := range fn.Imports {
			sb.WriteString(fmt.Sprintf("import %s\n", imp))
		}
		sb.WriteString("```\n\n")
	}

	if len(fn.TypeDefs) > 0 {
		sb.WriteString("### Type Definitions\n```go\n")
		for _, td := range fn.TypeDefs {
			sb.WriteString(td + "\n\n")
		}
		sb.WriteString("```\n\n")
	}

	sb.WriteString("### Function\n```go\n")
	sb.WriteString(fn.Signature + " ")
	sb.WriteString(fn.Body)
	sb.WriteString("\n```\n\n")

	sb.WriteString("### Diff (changes made)\n```diff\n")
	sb.WriteString(fn.DiffContext)
	sb.WriteString("\n```\n\n")

	sb.WriteString(`## Instructions

1. Identify 2-3 realistic mutations **scoped to the changed lines** in the diff. Focus on:
   - Off-by-one errors
   - Wrong comparison operators (< vs <=, == vs !=)
   - Missing or wrong error handling
   - Boundary condition errors
   - Missing nil/zero checks
   - Wrong variable references

2. For each mutation, provide:
   - A short description of the bug it introduces
   - The exact original code snippet (must be a substring of the function body)
   - The mutated code snippet (the replacement)

3. For each mutation, write a Go test using the standard "testing" package that:
   - PASSES on the original code
   - FAILS on the mutated code
   - Is a complete, compilable test file with proper package declaration and imports
   - Uses the test function name format: Test<FuncName>_<MutantDescription>

## Output Format

Respond with ONLY a JSON object (no markdown fences, no explanation) in this exact format:

{
  "mutants": [
    {
      "id": "m1",
      "description": "short description of the mutation",
      "original": "exact original code snippet",
      "mutated": "mutated code snippet"
    }
  ],
  "tests": [
    {
      "id": "t1",
      "mutant_id": "m1",
      "test_name": "TestFuncName_MutantDescription",
      "test_code": "package pkg\n\nimport (\n\t\"testing\"\n)\n\nfunc TestFuncName_MutantDescription(t *testing.T) {\n\t// test body\n}"
    }
  ]
}

IMPORTANT:
- The "original" field in each mutant MUST be an exact substring of the function body shown above
- Each test must be a complete, self-contained Go test file
- The package name in tests must be "` + fn.Package + `"
- Do not use any external test frameworks — only the standard "testing" package
- Ensure tests are deterministic (no randomness, no timing dependencies)
`)

	return sb.String()
}
