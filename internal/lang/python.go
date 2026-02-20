package lang

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/yiyuanh/snare/pkg/model"
)

//go:embed python_helper.py
var pythonHelperScript string

// Python implements the Language interface for Python codebases.
type Python struct{}

func NewPython() *Python {
	return &Python{}
}

func (p *Python) Name() string {
	return "python"
}

func (p *Python) FileExtensions() []string {
	return []string{".py"}
}

// pythonFuncInfo represents the JSON output from the Python helper script.
type pythonFuncInfo struct {
	Name      string   `json:"name"`
	Signature string   `json:"signature"`
	Body      string   `json:"body"`
	StartLine int      `json:"start_line"`
	EndLine   int      `json:"end_line"`
	Imports   []string `json:"imports"`
	Module    string   `json:"module"`
}

func (p *Python) IdentifyChangedFuncs(filePath string, source []byte, hunks []model.Hunk) ([]model.ChangedFunc, error) {
	// Write source to a temp file for the Python helper to parse
	tmpFile, err := os.CreateTemp("", "snare-py-*.py")
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(source); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("writing temp file: %w", err)
	}
	tmpFile.Close()

	// Run the Python helper script (its __main__ block handles argv and JSON output)
	cmd := exec.Command("python3", "-c", pythonHelperScript, tmpFile.Name())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("running Python helper: %w\nstderr: %s", err, stderr.String())
	}

	var funcs []pythonFuncInfo
	if err := json.Unmarshal(stdout.Bytes(), &funcs); err != nil {
		return nil, fmt.Errorf("parsing Python helper output: %w\noutput: %s", err, stdout.String())
	}

	// Determine module name from file path
	module := filepath.Base(filePath)
	module = strings.TrimSuffix(module, ".py")

	var result []model.ChangedFunc
	for _, fn := range funcs {
		if !overlapsHunksPython(fn, hunks) {
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
			Package:     module,
			Name:        fn.Name,
			Signature:   fn.Signature,
			Body:        fn.Body,
			StartLine:   fn.StartLine,
			EndLine:     fn.EndLine,
			Imports:     fn.Imports,
			DiffContext: strings.Join(diffParts, "\n"),
		})
	}
	return result, nil
}

func overlapsHunksPython(fn pythonFuncInfo, hunks []model.Hunk) bool {
	for _, h := range hunks {
		hunkEnd := h.NewStartLine + h.NewLineCount - 1
		if hunkEnd >= fn.StartLine && h.NewStartLine <= fn.EndLine {
			return true
		}
	}
	return false
}

func (p *Python) ApplyMutant(originalSource []byte, original string, mutated string) ([]byte, error) {
	result := strings.Replace(string(originalSource), original, mutated, 1)
	if result == string(originalSource) {
		return nil, fmt.Errorf("original snippet not found in source")
	}
	// Validate that the mutated source is still valid Python
	cmd := exec.Command("python3", "-c", fmt.Sprintf("import ast; ast.parse(%q)", result))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("mutated code is not valid Python: %s", stderr.String())
	}
	return []byte(result), nil
}

func (p *Python) RunTest(dir string, testFile string, testFunc string, timeout time.Duration) (passed bool, output string, err error) {
	timeoutSec := int(timeout.Seconds())
	if timeoutSec < 1 {
		timeoutSec = 1
	}

	args := []string{"-m", "pytest", "-xvs", fmt.Sprintf("%s::%s", testFile, testFunc), fmt.Sprintf("--timeout=%d", timeoutSec)}
	cmd := exec.Command("python3", args...)
	cmd.Dir = dir

	// Set PYTHONPATH to the temp dir root so imports work
	env := os.Environ()
	env = append(env, fmt.Sprintf("PYTHONPATH=%s", dir))
	cmd.Env = env

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err = cmd.Run()
	output = buf.String()

	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			// Test failed (non-zero exit) — expected for catching tests
			return false, output, nil
		}
		return false, output, fmt.Errorf("running test: %w", err)
	}
	return true, output, nil
}

func (p *Python) ValidateTestSyntax(testCode []byte) error {
	cmd := exec.Command("python3", "-c", fmt.Sprintf("import ast; ast.parse(%q)", string(testCode)))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("invalid Python syntax: %s", stderr.String())
	}
	return nil
}
