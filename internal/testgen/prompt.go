package testgen

import (
	"fmt"
	"strings"

	"github.com/yiyuanh/snare/pkg/model"
)

// BuildCatchingPrompt constructs the intent-aware catching prompt for Claude.
// It takes a ChangedFunc with both parent and new code populated,
// and an optional commit message for additional context.
func BuildCatchingPrompt(fn model.ChangedFunc, commitMessage ...string) string {
	// Detect language from file extension
	isPython := strings.HasSuffix(fn.FilePath, ".py")
	return buildCatchingPromptForLang(fn, isPython, commitMessage...)
}

func buildCatchingPromptForLang(fn model.ChangedFunc, isPython bool, commitMessage ...string) string {
	var sb strings.Builder

	codeLang := "go"
	testFramework := "Go test"
	testExample := `"package pkg\n\nimport (\n\t\"testing\"\n)\n\nfunc TestFuncName_RiskDescription(t *testing.T) {\n\t// test body\n}"`
	testNameExample := "TestFuncName_RiskDescription"
	packageLabel := "Package"
	if isPython {
		codeLang = "python"
		testFramework = "pytest"
		testExample = `"import pytest\nfrom module import function\n\ndef test_func_name_risk_description():\n    # test body\n    assert result == expected"`
		testNameExample = "test_func_name_risk_description"
		packageLabel = "Module"
	}

	sb.WriteString(`You are a Just-in-Time catching test expert. Your goal is to find bugs introduced by a code change by generating tests that pass on the OLD code but might fail on the NEW code.

`)

	sb.WriteString("## Context\n\n")
	sb.WriteString(fmt.Sprintf("%s: %s\n", packageLabel, fn.Package))
	sb.WriteString(fmt.Sprintf("File: %s\n\n", fn.FilePath))

	if len(fn.Imports) > 0 {
		sb.WriteString("### Imports\n```" + codeLang + "\n")
		for _, imp := range fn.Imports {
			if isPython {
				sb.WriteString(imp + "\n")
			} else {
				sb.WriteString(fmt.Sprintf("import %s\n", imp))
			}
		}
		sb.WriteString("```\n\n")
	}

	if len(fn.TypeDefs) > 0 {
		sb.WriteString("### Type Definitions\n```" + codeLang + "\n")
		for _, td := range fn.TypeDefs {
			sb.WriteString(td + "\n\n")
		}
		sb.WriteString("```\n\n")
	}

	// Commit context (if provided)
	if len(commitMessage) > 0 && commitMessage[0] != "" {
		sb.WriteString("### Commit Context\n")
		sb.WriteString(commitMessage[0])
		sb.WriteString("\n\n")
	}

	// Telemetry context (if available)
	if fn.TelemetryContext != "" {
		sb.WriteString("### Production Telemetry\n")
		sb.WriteString("The following production telemetry data provides context about how this function is used in practice:\n\n")
		sb.WriteString(fn.TelemetryContext)
		sb.WriteString("\n\n")
		sb.WriteString("Use this telemetry to prioritize risks that affect production usage patterns. ")
		sb.WriteString("High-traffic functions with known callers and endpoints deserve more thorough testing. ")
		sb.WriteString("Known exceptions and incidents should inform risk identification.\n\n")
	}

	// Parent (OLD) function
	sb.WriteString("### Parent (OLD) Function — this is the baseline, known-good code\n```" + codeLang + "\n")
	if fn.ParentSignature != "" {
		if isPython {
			sb.WriteString(fn.ParentBody)
		} else {
			sb.WriteString(fn.ParentSignature + " ")
			sb.WriteString(fn.ParentBody)
		}
	} else {
		if isPython {
			sb.WriteString(fn.Body)
		} else {
			sb.WriteString(fn.Signature + " ")
			sb.WriteString(fn.Body)
		}
	}
	sb.WriteString("\n```\n\n")

	// Current (NEW) function
	sb.WriteString("### Current (NEW) Function — this is the code change being tested\n```" + codeLang + "\n")
	if isPython {
		sb.WriteString(fn.Body)
	} else {
		sb.WriteString(fn.Signature + " ")
		sb.WriteString(fn.Body)
	}
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

4. **Generate Catching Tests**: For each mutant, write a ` + testFramework + ` test that:
   - PASSES on the parent (old) code
   - FAILS on the mutant
   - Tests the specific risk scenario
   - Is a complete, self-contained test file`)

	if !isPython {
		sb.WriteString(" with proper package declaration and imports")
	}

	sb.WriteString(`

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
      "test_name": "` + testNameExample + `",
      "test_code": ` + testExample + `
    }
  ]
}

IMPORTANT:
- The "original" field in each mutant MUST be an exact substring of the PARENT function body shown above`)

	if isPython {
		sb.WriteString(`
- Each test must be a complete, self-contained Python test file
- Import the function under test from the module "` + fn.Package + `"
- Use pytest assertions (assert statements), not unittest
- Ensure tests are deterministic (no randomness, no timing dependencies)
- Each mutant must reference a risk via "risk_id"
`)
	} else {
		sb.WriteString(`
- Each test must be a complete, self-contained Go test file
- The package name in tests must be "` + fn.Package + `"
- Do not use any external test frameworks — only the standard "testing" package
- Ensure tests are deterministic (no randomness, no timing dependencies)
- Each mutant must reference a risk via "risk_id"
`)
	}

	return sb.String()
}
