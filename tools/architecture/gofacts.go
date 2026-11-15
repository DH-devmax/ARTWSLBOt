// gofacts extracts source evidence only; it does not load or execute product code.
package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
)

type Evidence struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Symbol string `json:"symbol"`
}

type ScopedEvidence struct {
	Evidence
	Function string `json:"function"`
}

// Keep only selector/identifier names, never arguments, SQL, prompts or literals.
func reference(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		if left := reference(value.X); left != "" {
			return left + "." + value.Sel.Name
		}
	}
	return ""
}

func main() {
	root, _ := filepath.Abs(os.Args[1])
	var files []string
	if err := json.NewDecoder(os.Stdin).Decode(&files); err != nil {
		panic(err)
	}
	imports := map[string][]Evidence{}
	symbols := map[string][]Evidence{}
	calls := []ScopedEvidence{}
	bindings := []ScopedEvidence{}
	for _, name := range files {
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, filepath.Join(root, name), nil, 0)
		if err != nil {
			panic(err)
		}
		for _, imp := range file.Imports {
			value, _ := strconv.Unquote(imp.Path.Value)
			key := filepath.ToSlash(filepath.Dir(name)) + "->" + value
			imports[key] = append(imports[key], Evidence{name, set.Position(imp.Pos()).Line, value})
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch value := n.(type) {
			case *ast.FuncDecl:
				symbols[value.Name.Name] = append(symbols[value.Name.Name], Evidence{name, set.Position(value.Pos()).Line, value.Name.Name})
			case *ast.SelectorExpr:
				symbols[value.Sel.Name] = append(symbols[value.Sel.Name], Evidence{name, set.Position(value.Pos()).Line, value.Sel.Name})
			case *ast.KeyValueExpr:
				if key, ok := value.Key.(*ast.Ident); ok {
					symbols[key.Name] = append(symbols[key.Name], Evidence{name, set.Position(value.Pos()).Line, key.Name})
				}
			}
			return true
		})
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(n ast.Node) bool {
				switch value := n.(type) {
				case *ast.CallExpr:
					if target := reference(value.Fun); target != "" {
						calls = append(calls, ScopedEvidence{
							Evidence{name, set.Position(value.Pos()).Line, target}, function.Name.Name,
						})
					}
				case *ast.AssignStmt:
					if len(value.Lhs) == 1 && len(value.Rhs) == 1 {
						left, right := reference(value.Lhs[0]), reference(value.Rhs[0])
						if left != "" && right != "" {
							bindings = append(bindings, ScopedEvidence{
								Evidence{name, set.Position(value.Pos()).Line, left + " = " + right}, function.Name.Name,
							})
						}
					}
				}
				return true
			})
		}
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{
		"imports": imports, "symbols": symbols, "calls": calls, "bindings": bindings,
	})
}
