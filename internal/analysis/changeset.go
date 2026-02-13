package analysis

import (
	"fmt"
	"go/token"
	"os"
	"strings"

	"github.com/yiyuanh/snare/pkg/model"
)

// MapChangedFuncs takes file diffs and identifies which functions were changed.
func MapChangedFuncs(diffs []model.FileDiff) ([]model.ChangedFunc, error) {
	var result []model.ChangedFunc

	for _, fd := range diffs {
		funcs, err := analyzeFile(fd)
		if err != nil {
			return nil, fmt.Errorf("analyzing %s: %w", fd.NewName, err)
		}
		result = append(result, funcs...)
	}
	return result, nil
}

func analyzeFile(fd model.FileDiff) ([]model.ChangedFunc, error) {
	src, err := os.ReadFile(fd.NewName)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	fset := token.NewFileSet()
	pkg, imports, typeDefs, funcs, err := ParseFunctions(fset, src)
	if err != nil {
		return nil, fmt.Errorf("parsing AST: %w", err)
	}

	var result []model.ChangedFunc

	for _, fn := range funcs {
		overlapping := findOverlappingHunks(fn, fd.Hunks)
		if len(overlapping) == 0 {
			continue
		}

		var diffParts []string
		for _, h := range overlapping {
			diffParts = append(diffParts, h.Content)
		}

		result = append(result, model.ChangedFunc{
			FilePath:    fd.NewName,
			Package:     pkg,
			Name:        fn.Name,
			Signature:   fn.Signature,
			Body:        fn.Body,
			StartLine:   fn.StartLine,
			EndLine:     fn.EndLine,
			Imports:     imports,
			TypeDefs:    typeDefs,
			DiffContext: strings.Join(diffParts, "\n"),
		})
	}
	return result, nil
}

// findOverlappingHunks returns hunks whose new-file line range overlaps
// with the function's line range.
func findOverlappingHunks(fn FuncInfo, hunks []model.Hunk) []model.Hunk {
	var result []model.Hunk
	for _, h := range hunks {
		hunkStart := h.NewStartLine
		hunkEnd := h.NewStartLine + h.NewLineCount - 1
		if hunkEnd < fn.StartLine || hunkStart > fn.EndLine {
			continue
		}
		result = append(result, h)
	}
	return result
}
