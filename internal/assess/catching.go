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
	if strings.Contains(result.OriginalOutput, "build failed") ||
		strings.Contains(result.OriginalOutput, "does not compile") ||
		strings.Contains(result.OriginalOutput, "[build failed]") {
		result.FilteredReason = "compilation failure"
		result.IsCatching = false
		result.Confidence = 0
	}
}

// CatchingAssessor identifies tests that pass on original and fail on mutant.
type CatchingAssessor struct{}

func (a *CatchingAssessor) Assess(result *model.TestResult) {
	if result.FilteredReason != "" {
		return
	}
	if !result.PassOriginal {
		result.FilteredReason = "fails on original code"
		result.IsCatching = false
		result.Confidence = 0
		return
	}
	if !result.FailMutant {
		result.FilteredReason = "does not detect mutation"
		result.IsCatching = false
		result.Confidence = 0
		return
	}
	result.IsCatching = true
	result.Confidence = 1.0
}

// TrivialMutantFilter reduces confidence for trivial mutations.
type TrivialMutantFilter struct{}

func (f *TrivialMutantFilter) Assess(result *model.TestResult) {
	if result.FilteredReason != "" || !result.IsCatching {
		return
	}

	// If the mutant is extremely small or trivial, reduce confidence
	original := strings.TrimSpace(result.Mutant.Original)
	mutated := strings.TrimSpace(result.Mutant.Mutated)

	if len(original) < 3 || len(mutated) < 3 {
		result.Confidence *= 0.5
	}

	// If mutant just removes code entirely
	if mutated == "" || mutated == "{}" {
		result.Confidence *= 0.7
	}
}

// ErrorMessageFilter adjusts confidence based on error message quality.
type ErrorMessageFilter struct{}

func (f *ErrorMessageFilter) Assess(result *model.TestResult) {
	if result.FilteredReason != "" || !result.IsCatching {
		return
	}

	// Tests with clear assertion failures are higher confidence
	output := result.MutantOutput
	if strings.Contains(output, "FAIL") && (strings.Contains(output, "got") || strings.Contains(output, "expected") || strings.Contains(output, "want")) {
		result.Confidence = min(result.Confidence*1.1, 1.0)
	}

	// Panic-based failures are slightly lower confidence
	if strings.Contains(output, "panic:") {
		result.Confidence *= 0.9
	}
}
