package analysis

import (
	"go/token"
	"testing"
)

func TestParseFunctions_Basic(t *testing.T) {
	src := []byte(`package example

import (
	"fmt"
	"strings"
)

type Config struct {
	Name string
}

func Hello(name string) string {
	return fmt.Sprintf("hello %s", strings.ToLower(name))
}
`)

	fset := token.NewFileSet()
	pkg, imports, typeDefs, funcs, err := ParseFunctions(fset, src)
	if err != nil {
		t.Fatalf("ParseFunctions returned error: %v", err)
	}

	if pkg != "example" {
		t.Errorf("package = %q, want %q", pkg, "example")
	}

	if len(imports) != 2 {
		t.Fatalf("len(imports) = %d, want 2", len(imports))
	}
	if imports[0] != `"fmt"` {
		t.Errorf("imports[0] = %q, want %q", imports[0], `"fmt"`)
	}
	if imports[1] != `"strings"` {
		t.Errorf("imports[1] = %q, want %q", imports[1], `"strings"`)
	}

	if len(typeDefs) != 1 {
		t.Fatalf("len(typeDefs) = %d, want 1", len(typeDefs))
	}
	if typeDefs[0] == "" {
		t.Error("typeDefs[0] is empty")
	}

	if len(funcs) != 1 {
		t.Fatalf("len(funcs) = %d, want 1", len(funcs))
	}
	fn := funcs[0]
	if fn.Name != "Hello" {
		t.Errorf("func name = %q, want %q", fn.Name, "Hello")
	}
	if fn.StartLine == 0 || fn.EndLine == 0 {
		t.Errorf("line range not set: start=%d end=%d", fn.StartLine, fn.EndLine)
	}
	if fn.StartLine > fn.EndLine {
		t.Errorf("start line %d > end line %d", fn.StartLine, fn.EndLine)
	}
	if fn.Body == "" {
		t.Error("body is empty")
	}
	if fn.Signature == "" {
		t.Error("signature is empty")
	}
}

func TestParseFunctions_Method(t *testing.T) {
	src := []byte(`package example

type Server struct {
	Port int
}

func (s *Server) Start() error {
	return nil
}
`)

	fset := token.NewFileSet()
	_, _, _, funcs, err := ParseFunctions(fset, src)
	if err != nil {
		t.Fatalf("ParseFunctions returned error: %v", err)
	}

	if len(funcs) != 1 {
		t.Fatalf("len(funcs) = %d, want 1", len(funcs))
	}

	fn := funcs[0]
	if fn.Name != "Start" {
		t.Errorf("func name = %q, want %q", fn.Name, "Start")
	}
	// Signature should include the receiver
	if fn.Signature == "" {
		t.Error("signature is empty")
	}
	// Should contain the receiver type
	if !contains(fn.Signature, "*Server") {
		t.Errorf("signature %q does not contain receiver *Server", fn.Signature)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
