package lang

import (
	"time"

	"github.com/yiyuanh/snare/pkg/model"
)

// Language defines the interface for language-specific operations.
// This provides an extensibility seam for supporting languages beyond Go.
type Language interface {
	Name() string
	FileExtensions() []string
	IdentifyChangedFuncs(filePath string, source []byte, hunks []model.Hunk) ([]model.ChangedFunc, error)
	ApplyMutant(originalSource []byte, original string, mutated string) ([]byte, error)
	RunTest(dir string, testFile string, testFunc string, timeout time.Duration) (passed bool, output string, err error)
	ValidateTestSyntax(testCode []byte) error
}
