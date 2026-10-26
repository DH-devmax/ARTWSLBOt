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

func main() {
	root, _ := filepath.Abs(os.Args[1])
	var files []string
	if err := json.NewDecoder(os.Stdin).Decode(&files); err != nil {
		panic(err)
	}
	imports := map[string][]Evidence{}
	symbols := map[string][]Evidence{}
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
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{"imports": imports, "symbols": symbols})
}
