package lang

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/yiyuanh/snare/internal/analysis"
	"github.com/yiyuanh/snare/pkg/model"
)

// Go implements the Language interface for Go codebases.
type Go struct{}

func NewGo() *Go {
	return &Go{}
}

func (g *Go) Name() string {
	return "go"
}

func (g *Go) FileExtensions() []string {
	return []string{".go"}
}

func (g *Go) IdentifyChangedFuncs(filePath string, source []byte, hunks []model.Hunk) ([]model.ChangedFunc, error) {
	fset := token.NewFileSet()
	pkg, imports, typeDefs, funcs, err := analysis.ParseFunctions(fset, source)
	if err != nil {
		return nil, fmt.Errorf("parsing AST: %w", err)
	}

	var result []model.ChangedFunc
	for _, fn := range funcs {
		if !overlapsHunks(fn, hunks) {
			continue
		}

		var diffParts []string
		for _, h := range hunks {
			hunkEnd := h.NewStartLine + h.NewLineCount - 1
			if hunkEnd >= fn.StartLine && h.NewStartLine <= fn.EndLine {
				diffParts = append(diffParts, h.Content)
			}
		}

		result = append(result, model.ChangedFunc{
			FilePath:    filePath,
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

func overlapsHunks(fn analysis.FuncInfo, hunks []model.Hunk) bool {
	for _, h := range hunks {
		hunkEnd := h.NewStartLine + h.NewLineCount - 1
		if hunkEnd >= fn.StartLine && h.NewStartLine <= fn.EndLine {
			return true
		}
	}
	return false
}

func (g *Go) ApplyMutant(originalSource []byte, original string, mutated string) ([]byte, error) {
	result := strings.Replace(string(originalSource), original, mutated, 1)
	if result == string(originalSource) {
		return nil, fmt.Errorf("original snippet not found in source")
	}
	return []byte(result), nil
}

func (g *Go) RunTest(dir string, testFile string, testFunc string, timeout time.Duration) (passed bool, output string, err error) {
	// Determine the package directory from the test file
	pkgDir := filepath.Dir(testFile)

	args := []string{"test", "-v", "-count=1", fmt.Sprintf("-timeout=%s", timeout), "-run", fmt.Sprintf("^%s$", testFunc), pkgDir}
	cmd := exec.Command("go", args...)
	cmd.Dir = dir

	// Merge stdout and stderr
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	// Set up environment to ensure we use the temp dir's go.mod
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")

	err = cmd.Run()
	output = buf.String()

	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			// Test failed (non-zero exit) — this is expected for mutant tests
			return false, output, nil
		}
		return false, output, fmt.Errorf("running test: %w", err)
	}
	return true, output, nil
}

func (g *Go) ValidateTestSyntax(testCode []byte) error {
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, "test.go", testCode, parser.AllErrors)
	return err
}
