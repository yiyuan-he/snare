package analysis

import (
	"testing"

	"github.com/yiyuanh/snare/pkg/model"
)

func TestFindOverlappingHunks_Overlap(t *testing.T) {
	fn := FuncInfo{
		Name:      "Foo",
		StartLine: 10,
		EndLine:   20,
	}

	hunks := []model.Hunk{
		{NewStartLine: 15, NewLineCount: 3, Content: "hunk1"},
		{NewStartLine: 50, NewLineCount: 5, Content: "hunk2"},
	}

	result := findOverlappingHunks(fn, hunks)
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}
	if result[0].Content != "hunk1" {
		t.Errorf("result[0].Content = %q, want %q", result[0].Content, "hunk1")
	}
}

func TestFindOverlappingHunks_PartialOverlap(t *testing.T) {
	fn := FuncInfo{
		Name:      "Bar",
		StartLine: 10,
		EndLine:   20,
	}

	// Hunk starts before function but extends into it
	hunks := []model.Hunk{
		{NewStartLine: 5, NewLineCount: 10, Content: "partial"},
	}

	result := findOverlappingHunks(fn, hunks)
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}
	if result[0].Content != "partial" {
		t.Errorf("result[0].Content = %q, want %q", result[0].Content, "partial")
	}
}

func TestFindOverlappingHunks_NoOverlap(t *testing.T) {
	fn := FuncInfo{
		Name:      "Baz",
		StartLine: 10,
		EndLine:   20,
	}

	hunks := []model.Hunk{
		{NewStartLine: 1, NewLineCount: 5, Content: "before"},
		{NewStartLine: 25, NewLineCount: 3, Content: "after"},
	}

	result := findOverlappingHunks(fn, hunks)
	if len(result) != 0 {
		t.Fatalf("len(result) = %d, want 0", len(result))
	}
}

func TestFindOverlappingHunks_ExactBoundary(t *testing.T) {
	fn := FuncInfo{
		Name:      "Edge",
		StartLine: 10,
		EndLine:   20,
	}

	// Hunk ends exactly at function start line
	hunks := []model.Hunk{
		{NewStartLine: 8, NewLineCount: 3, Content: "boundary"},
	}
	// hunkEnd = 8 + 3 - 1 = 10, fn.StartLine = 10 → overlaps

	result := findOverlappingHunks(fn, hunks)
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}
}
