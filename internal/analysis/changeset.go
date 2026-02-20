package analysis

import (
	"fmt"
	"go/token"
	"os"
	"strings"

	"github.com/yiyuanh/snare/pkg/model"
)

// FuncIdentifier is a minimal interface for identifying changed functions.
// This avoids importing the lang package to prevent import cycles.
type FuncIdentifier interface {
	IdentifyChangedFuncs(filePath string, source []byte, hunks []model.Hunk) ([]model.ChangedFunc, error)
}

// MapChangedFuncs takes file diffs and identifies which functions were changed.
// It analyzes both the new code and parent code to populate dual-version info.
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

// MapChangedFuncsWithLang uses the FuncIdentifier interface to identify changed functions.
// This supports languages beyond Go (e.g., Python).
func MapChangedFuncsWithLang(diffs []model.FileDiff, language FuncIdentifier) ([]model.ChangedFunc, error) {
	var result []model.ChangedFunc

	for _, fd := range diffs {
		// Read the new source from disk or from the diff
		var newSrc []byte
		if len(fd.NewSource) > 0 {
			newSrc = fd.NewSource
		} else {
			var err error
			newSrc, err = os.ReadFile(fd.NewName)
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", fd.NewName, err)
			}
		}

		// Use the language interface to identify changed functions in the new source
		changedFuncs, err := language.IdentifyChangedFuncs(fd.NewName, newSrc, fd.Hunks)
		if err != nil {
			return nil, fmt.Errorf("identifying changed functions in %s: %w", fd.NewName, err)
		}

		// If parent source is available, also parse it to populate parent fields.
		// Use a broad hunk covering the entire file so all functions are extracted
		// (new-file hunks have wrong line numbers for the parent source).
		if len(fd.ParentSource) > 0 {
			parentLines := strings.Count(string(fd.ParentSource), "\n") + 1
			allHunks := []model.Hunk{{NewStartLine: 1, NewLineCount: parentLines}}
			parentFuncs, err := language.IdentifyChangedFuncs(fd.NewName, fd.ParentSource, allHunks)
			if err == nil {
				// Build parent lookup by name
				parentMap := make(map[string]model.ChangedFunc)
				for _, pf := range parentFuncs {
					parentMap[pf.Name] = pf
				}

				// Populate parent info and filter out new functions
				var filtered []model.ChangedFunc
				for _, cf := range changedFuncs {
					if pf, ok := parentMap[cf.Name]; ok {
						cf.ParentSignature = pf.Signature
						cf.ParentBody = pf.Body
						filtered = append(filtered, cf)
					}
					// Skip functions with no parent — no regression possible
				}
				changedFuncs = filtered
			}
		}

		result = append(result, changedFuncs...)
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

	// Parse parent source if available
	var parentFuncs []FuncInfo
	if len(fd.ParentSource) > 0 {
		parentFset := token.NewFileSet()
		_, _, _, pf, err := ParseFunctions(parentFset, fd.ParentSource)
		if err == nil {
			parentFuncs = pf
		}
	}

	// Build lookup for parent functions by name
	parentFuncMap := make(map[string]FuncInfo)
	for _, pf := range parentFuncs {
		parentFuncMap[pf.Name] = pf
	}

	var result []model.ChangedFunc

	for _, fn := range funcs {
		overlapping := findOverlappingHunks(fn, fd.Hunks)
		if len(overlapping) == 0 {
			continue
		}

		// Skip newly added functions (no parent version) — no regression possible
		parentFunc, hasParent := parentFuncMap[fn.Name]
		if !hasParent && len(fd.ParentSource) > 0 {
			continue
		}

		var diffParts []string
		for _, h := range overlapping {
			diffParts = append(diffParts, h.Content)
		}

		cf := model.ChangedFunc{
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
		}

		// Populate parent info if available
		if hasParent {
			cf.ParentSignature = parentFunc.Signature
			cf.ParentBody = parentFunc.Body
		}

		result = append(result, cf)
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
