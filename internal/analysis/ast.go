package analysis

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
)

// FuncInfo holds information about a function declaration found by AST parsing.
type FuncInfo struct {
	Name      string
	Signature string
	Body      string
	StartLine int
	EndLine   int
}

// ParseFunctions parses a Go source file and returns info about all function declarations.
func ParseFunctions(fset *token.FileSet, src []byte) (pkg string, imports []string, typeDefs []string, funcs []FuncInfo, err error) {
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return "", nil, nil, nil, err
	}

	pkg = file.Name.Name

	// Extract imports
	for _, imp := range file.Imports {
		path := imp.Path.Value
		if imp.Name != nil {
			path = imp.Name.Name + " " + path
		}
		imports = append(imports, path)
	}

	// Extract type definitions
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}
		var buf bytes.Buffer
		if err := format.Node(&buf, fset, genDecl); err == nil {
			typeDefs = append(typeDefs, buf.String())
		}
	}

	// Extract functions
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		startPos := fset.Position(funcDecl.Pos())
		endPos := fset.Position(funcDecl.End())

		// Build signature
		var sigBuf bytes.Buffer
		// Include receiver if present
		sigBuf.WriteString("func ")
		if funcDecl.Recv != nil {
			sigBuf.WriteString("(")
			for i, field := range funcDecl.Recv.List {
				if i > 0 {
					sigBuf.WriteString(", ")
				}
				var tb bytes.Buffer
				format.Node(&tb, fset, field.Type)
				if len(field.Names) > 0 {
					sigBuf.WriteString(field.Names[0].Name + " ")
				}
				sigBuf.WriteString(tb.String())
			}
			sigBuf.WriteString(") ")
		}
		sigBuf.WriteString(funcDecl.Name.Name)

		// Params
		var paramBuf bytes.Buffer
		format.Node(&paramBuf, fset, funcDecl.Type)
		// paramBuf contains "func(...) ..." — extract the params portion
		paramStr := paramBuf.String()
		if idx := strings.Index(paramStr, "("); idx >= 0 {
			sigBuf.WriteString(paramStr[idx:])
		}

		// Body
		var bodyBuf bytes.Buffer
		if funcDecl.Body != nil {
			format.Node(&bodyBuf, fset, funcDecl.Body)
		}

		funcs = append(funcs, FuncInfo{
			Name:      funcDecl.Name.Name,
			Signature: sigBuf.String(),
			Body:      bodyBuf.String(),
			StartLine: startPos.Line,
			EndLine:   endPos.Line,
		})
	}

	return pkg, imports, typeDefs, funcs, nil
}
