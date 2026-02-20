package assess

import (
	"strings"

	"github.com/yiyuanh/snare/pkg/model"
)

// CompilationFilter filters out tests that failed to compile.
type CompilationFilter struct{}

func (f *CompilationFilter) Assess(result *model.TestResult) {
	if result.FilteredReason != "" {
		return
	}
	if strings.Contains(result.ParentOutput, "build failed") ||
		strings.Contains(result.ParentOutput, "does not compile") ||
		strings.Contains(result.ParentOutput, "[build failed]") {
		result.FilteredReason = "compilation failure"
		result.IsCatching = false
		result.Confidence = 0
		result.Assessment = -1
	}
}

// CatchingAssessor identifies tests that pass on parent and fail on new code.
type CatchingAssessor struct{}

func (a *CatchingAssessor) Assess(result *model.TestResult) {
	if result.FilteredReason != "" {
		return
	}
	if !result.PassParent {
		result.FilteredReason = "fails on parent code"
		result.IsCatching = false
		result.Confidence = 0
		result.Assessment = -1
		return
	}
	if !result.FailDiff {
		// No behavioral change detected
		result.IsCatching = false
		result.Confidence = 0
		result.Assessment = 0
		return
	}
	result.IsCatching = true
	result.Confidence = 1.0
	result.Assessment = 0.5 // neutral starting point for weak catch
}

// FalsePositivePatterns implements patterns from the catching paper Table 2.
// Each pattern checks for common false positive indicators and reduces assessment.
type FalsePositivePatterns struct{}

func (f *FalsePositivePatterns) Assess(result *model.TestResult) {
	if result.FilteredReason != "" || !result.IsCatching {
		return
	}

	testCode := result.Test.TestCode
	output := result.DiffOutput

	// reflection: test uses reflection — likely brittle
	if strings.Contains(testCode, "reflect.") {
		result.Assessment -= 0.3
	}

	// type_mismatch: failure due to type changes
	if strings.Contains(output, "cannot use") || strings.Contains(output, "type mismatch") {
		result.Assessment -= 0.4
	}

	// bad_mock: incorrect mock setup
	if strings.Contains(output, "mock") && (strings.Contains(output, "unexpected call") || strings.Contains(output, "not set up")) {
		result.Assessment -= 0.4
	}

	// key_value_pair_change: ordering-dependent assertion
	if strings.Contains(output, "map[") && strings.Contains(testCode, "fmt.Sprint") {
		result.Assessment -= 0.3
	}

	// not_implemented_exception: intentional stub
	if strings.Contains(output, "not implemented") || strings.Contains(output, "TODO") {
		result.Assessment -= 0.5
	}

	// undefined_variable: variable removed/renamed
	if strings.Contains(output, "undefined:") || strings.Contains(output, "undeclared name") {
		result.Assessment -= 0.5
		result.FilteredReason = "undefined variable (likely rename)"
		result.IsCatching = false
	}

	// infrastructure_failure: test runner/infra issue
	if strings.Contains(output, "cannot find package") ||
		strings.Contains(output, "connection refused") ||
		strings.Contains(output, "timed out") ||
		strings.Contains(output, "Timeout >") {
		result.Assessment -= 0.6
		result.FilteredReason = "infrastructure failure"
		result.IsCatching = false
	}

	// Clamp assessment
	if result.Assessment < -1 {
		result.Assessment = -1
	}
}

// TruePositivePatterns implements patterns from the catching paper Table 3.
// Each pattern checks for indicators of genuine behavioral changes.
type TruePositivePatterns struct{}

func (f *TruePositivePatterns) Assess(result *model.TestResult) {
	if result.FilteredReason != "" || !result.IsCatching {
		return
	}

	output := result.DiffOutput

	// changed_bool: boolean value flip
	if (strings.Contains(output, "got: true") && strings.Contains(output, "want: false")) ||
		(strings.Contains(output, "got: false") && strings.Contains(output, "want: true")) ||
		(strings.Contains(output, "got true") && strings.Contains(output, "expected false")) ||
		(strings.Contains(output, "got false") && strings.Contains(output, "expected true")) {
		result.Assessment += 0.2
	}

	// null_value: value becomes nil unexpectedly
	if strings.Contains(output, "got: <nil>") || strings.Contains(output, "got <nil>") ||
		strings.Contains(output, "nil pointer dereference") {
		result.Assessment += 0.2
	}

	// empty_container: collection becomes empty
	if strings.Contains(output, "got: []") || strings.Contains(output, "got []") ||
		strings.Contains(output, "len(") && strings.Contains(output, "= 0") {
		result.Assessment += 0.2
	}

	// unexpected_key_change: key access failure
	if strings.Contains(output, "key not found") || strings.Contains(output, "index out of range") {
		result.Assessment += 0.15
	}

	// monotonic_change: existing behavior changed (clear assertion failure)
	if strings.Contains(output, "FAIL") &&
		(strings.Contains(output, "got") || strings.Contains(output, "expected") || strings.Contains(output, "want")) {
		result.Assessment += 0.1
	}

	// Clamp assessment
	if result.Assessment > 1 {
		result.Assessment = 1
	}
}
