// Command ci_cover_manifest classifies only the build-selected Go files on stdin.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxInputBytes     = 2 * 1024 * 1024
	maxFiles          = 4096
	failureDiagnostic = "coverage manifest validation failed"
)

var errInvalid = errors.New(failureDiagnostic)

type fileResult struct {
	Package      string `json:"package"`
	Instrumented bool   `json:"instrumented"`
}

type manifest struct {
	Files map[string]fileResult `json:"files"`
}

func validRelative(value string) bool {
	if value == "" || !utf8.ValidString(value) || strings.ContainsAny(value, "\\:") {
		return false
	}
	for _, character := range value {
		if unicode.IsSpace(character) || unicode.IsControl(character) || character == utf8.RuneError {
			return false
		}
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func validSourcePath(name string) bool {
	if !validRelative(name) || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
		return false
	}
	root, _, found := strings.Cut(name, "/")
	return found && (root == "cmd" || root == "internal" || root == "tools")
}

func readFiles(input io.Reader) (map[string]string, error) {
	raw, err := io.ReadAll(io.LimitReader(input, maxInputBytes+1))
	if err != nil || len(raw) > maxInputBytes || !utf8.Valid(raw) {
		return nil, errInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if value, err := decoder.Token(); err != nil || value != json.Delim('{') {
		return nil, errInvalid
	}
	if value, err := decoder.Token(); err != nil || value != "files" {
		return nil, errInvalid
	}
	if value, err := decoder.Token(); err != nil || value != json.Delim('{') {
		return nil, errInvalid
	}
	files := make(map[string]string)
	for decoder.More() {
		value, err := decoder.Token()
		name, ok := value.(string)
		if err != nil || !ok || !validSourcePath(name) || len(files) >= maxFiles {
			return nil, errInvalid
		}
		if _, exists := files[name]; exists {
			return nil, errInvalid
		}
		var packagePath string
		if err := decoder.Decode(&packagePath); err != nil || !validRelative(packagePath) {
			return nil, errInvalid
		}
		files[name] = packagePath
	}
	if value, err := decoder.Token(); err != nil || value != json.Delim('}') {
		return nil, errInvalid
	}
	if value, err := decoder.Token(); err != nil || value != json.Delim('}') {
		return nil, errInvalid
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errInvalid
	}
	return files, nil
}

func sourceInfo(root *os.Root, name string) (os.FileInfo, error) {
	parts := strings.Split(name, "/")
	for index := range parts {
		info, err := root.Lstat(strings.Join(parts[:index+1], "/"))
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return nil, errInvalid
		}
		if index == len(parts)-1 {
			if !info.Mode().IsRegular() {
				return nil, errInvalid
			}
			return info, nil
		}
		if !info.IsDir() {
			return nil, errInvalid
		}
	}
	return nil, errInvalid
}

func instrumentedFile(root *os.Root, name string) (bool, error) {
	before, err := sourceInfo(root, name)
	if err != nil {
		return false, errInvalid
	}
	// Root confines the open even if a checked path changes concurrently.
	source, err := root.Open(name)
	if err != nil {
		return false, errInvalid
	}
	defer source.Close()
	opened, err := source.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return false, errInvalid
	}
	file, err := parser.ParseFile(token.NewFileSet(), "", source, parser.SkipObjectResolution)
	if err != nil {
		return false, errInvalid
	}
	after, err := sourceInfo(root, name)
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() ||
		!before.ModTime().Equal(after.ModTime()) {
		return false, errInvalid
	}
	instrumented := false
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncDecl:
			instrumented = instrumented || node.Body != nil
		case *ast.FuncLit:
			instrumented = true
		}
		return !instrumented
	})
	return instrumented, nil
}

func buildManifest(directory string, input io.Reader) (manifest, error) {
	files, err := readFiles(input)
	if err != nil {
		return manifest{}, errInvalid
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return manifest{}, errInvalid
	}
	defer root.Close()
	result := manifest{Files: make(map[string]fileResult, len(files))}
	for name, packagePath := range files {
		instrumented, err := instrumentedFile(root, name)
		if err != nil {
			return manifest{}, errInvalid
		}
		result.Files[name] = fileResult{Package: packagePath, Instrumented: instrumented}
	}
	return result, nil
}

func run(directory string, input io.Reader, output, diagnostics io.Writer) int {
	result, err := buildManifest(directory, input)
	if err == nil {
		err = json.NewEncoder(output).Encode(result)
	}
	if err != nil {
		fmt.Fprintln(diagnostics, failureDiagnostic)
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(".", os.Stdin, os.Stdout, os.Stderr))
}
