package testgen

import (
	"fmt"
	"strings"

	"github.com/yiyuanh/snare/pkg/model"
)

// BuildCatchingPrompt constructs the intent-aware catching prompt for Claude.
// It takes a ChangedFunc with both parent and new code populated.
func BuildCatchingPrompt(fn model.ChangedFunc) string {
	var sb strings.Builder

	sb.WriteString(`You are a Just-in-Time catching test expert. Your goal is to find bugs introduced by a code change by generating tests that pass on the OLD code but might fail on the NEW code.

`)

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

	// Parent (OLD) function
	sb.WriteString("### Parent (OLD) Function — this is the baseline, known-good code\n```go\n")
	if fn.ParentSignature != "" {
		sb.WriteString(fn.ParentSignature + " ")
		sb.WriteString(fn.ParentBody)
	} else {
		// Fallback: use current code if no parent available
		sb.WriteString(fn.Signature + " ")
		sb.WriteString(fn.Body)
	}
	sb.WriteString("\n```\n\n")

	// Current (NEW) function
	sb.WriteString("### Current (NEW) Function — this is the code change being tested\n```go\n")
	sb.WriteString(fn.Signature + " ")
	sb.WriteString(fn.Body)
	sb.WriteString("\n```\n\n")

	// Diff context
	sb.WriteString("### Diff\n```diff\n")
	sb.WriteString(fn.DiffContext)
	sb.WriteString("\n```\n\n")

	sb.WriteString(`## Instructions

1. **Infer Intent**: Describe what this code change is trying to accomplish.

2. **Identify Risks**: List 2-4 realistic ways the implementation could introduce bugs while attempting this intent. Focus on:
   - Unintended side effects on existing behavior
   - Edge cases the change might break
   - Semantic errors (correct syntax but wrong logic)
   - Boundary condition regressions

3. **Generate Risk Mutants**: For each risk, create a mutant of the PARENT (old) function that represents the risk materializing. The mutant simulates what the code would look like if that specific bug were introduced.
   - The "original" field must be an exact substring of the PARENT function body
   - The "mutated" field is the buggy replacement

4. **Generate Catching Tests**: For each mutant, write a Go test that:
   - PASSES on the parent (old) code
   - FAILS on the mutant
   - Tests the specific risk scenario
   - Is a complete, compilable test file with proper package declaration and imports

## Output Format

Respond with ONLY a JSON object (no markdown fences, no explanation) in this exact format:

{
  "intent": "description of what the code change is trying to accomplish",
  "risks": [
    {
      "id": "r1",
      "description": "description of the risk"
    }
  ],
  "mutants": [
    {
      "id": "m1",
      "risk_id": "r1",
      "description": "short description of the mutation representing this risk",
      "original": "exact original code snippet from PARENT function body",
      "mutated": "mutated code snippet"
    }
  ],
  "tests": [
    {
      "id": "t1",
      "mutant_id": "m1",
      "test_name": "TestFuncName_RiskDescription",
      "test_code": "package pkg\n\nimport (\n\t\"testing\"\n)\n\nfunc TestFuncName_RiskDescription(t *testing.T) {\n\t// test body\n}"
    }
  ]
}

IMPORTANT:
- The "original" field in each mutant MUST be an exact substring of the PARENT function body shown above
- Each test must be a complete, self-contained Go test file
- The package name in tests must be "` + fn.Package + `"
- Do not use any external test frameworks — only the standard "testing" package
- Ensure tests are deterministic (no randomness, no timing dependencies)
- Each mutant must reference a risk via "risk_id"
`)

	return sb.String()
}
