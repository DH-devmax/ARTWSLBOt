package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeSource(t *testing.T, root, name, source string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}

func request(t *testing.T, files map[string]string) []byte {
	t.Helper()
	raw, err := json.Marshal(struct {
		Files map[string]string `json:"files"`
	}{Files: files})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func requireFailure(t *testing.T, root string, input io.Reader) {
	t.Helper()
	var output, diagnostics bytes.Buffer
	if code := run(root, input, &output, &diagnostics); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if output.Len() != 0 {
		t.Fatalf("failed request emitted stdout: %q", output.String())
	}
	if diagnostics.String() != failureDiagnostic+"\n" {
		t.Fatalf("diagnostic = %q", diagnostics.String())
	}
}

func TestManifestClassifiesActualFunctions(t *testing.T) {
	tests := []struct {
		name         string
		source       string
		instrumented bool
	}{
		{"declarations", "type Record struct{ Value int }; const Version = 1; var Count int", false},
		{"function type", "type Callback func(int) int", false},
		{"function alias", "type Callback = func()", false},
		{"generic function type", "type Callback[T any] func(T) T", false},
		{"function variable", "var Callback func()", false},
		{"interface method", "type Service interface { Call(func()) func() }", false},
		{"bodiless declaration", "func Foreign(callback func())", false},
		{"bodiless method", "type Record struct{}; func (Record) Foreign()", false},
		{"empty function", "func Empty() {}", true},
		{"function body", "func Value() int { return 1 }", true},
		{"empty method", "type Record struct{}; func (*Record) Empty() {}", true},
		{"generic function", "func Identity[T any](value T) T { return value }", true},
		{"initializer", "func init() {}", true},
		{"global literal", "var Callback = func() {}", true},
		{"typed global literal", "var Callback func() = func() {}", true},
		{"composite literal", "var Callbacks = []func(){func() {}}", true},
		{"nested literal", "var Value = (func() int { return (func() int { return 1 })() })()", true},
		{"literal with function type", "var Callback = func(next func()) func() { return next }", true},
		{"comments and strings", "// func Fake() {}\n/* var Fake = func() {} */\n" +
			"const Raw = `func Fake() { return func() {} }`\nconst Quoted = \"func Fake() {}\"", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			const name = "internal/sample/input.go"
			const packagePath = "example/internal/sample"
			writeSource(t, root, name, "package sample\n"+test.source+"\n")
			var output, diagnostics bytes.Buffer
			code := run(root, bytes.NewReader(request(t, map[string]string{name: packagePath})),
				&output, &diagnostics)
			if code != 0 || diagnostics.Len() != 0 {
				t.Fatalf("run failed: code=%d, diagnostic=%q", code, diagnostics.String())
			}
			var result manifest
			if err := json.Unmarshal(output.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			expected := map[string]fileResult{
				name: {Package: packagePath, Instrumented: test.instrumented},
			}
			if !reflect.DeepEqual(result.Files, expected) {
				t.Fatalf("files = %#v, want %#v", result.Files, expected)
			}
		})
	}
}

func TestManifestParsesOnlySuppliedBuildSelection(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"cmd/sample/main.go":       "example/cmd/sample",
		"internal/sample/cgo.go":   "example/internal/sample",
		"tools/sample/selected.go": "example/tools/sample",
	}
	writeSource(t, root, "cmd/sample/main.go", "package main\nfunc main() {}\n")
	writeSource(t, root, "internal/sample/cgo.go",
		"package sample\n/* int external(void); */\nimport \"C\"\nfunc call() { C.external() }\n")
	writeSource(t, root, "tools/sample/selected.go",
		"//go:build caller_selected\n\npackage sample\ntype Callback func()\n")
	writeSource(t, root, "internal/sample/unselected.go", "UNSELECTED INVALID SOURCE")
	writeSource(t, root, "tools/sample/unselected_test.go", "UNSELECTED INVALID TEST SOURCE")
	result, err := buildManifest(root, bytes.NewReader(request(t, files)))
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]fileResult{
		"cmd/sample/main.go":       {Package: files["cmd/sample/main.go"], Instrumented: true},
		"internal/sample/cgo.go":   {Package: files["internal/sample/cgo.go"], Instrumented: true},
		"tools/sample/selected.go": {Package: files["tools/sample/selected.go"], Instrumented: false},
	}
	if !reflect.DeepEqual(result.Files, expected) {
		t.Fatalf("files = %#v, want %#v", result.Files, expected)
	}
}

func TestManifestAcceptsEmptySelection(t *testing.T) {
	var output, diagnostics bytes.Buffer
	if code := run(t.TempDir(), strings.NewReader(`{"files":{}}`), &output, &diagnostics); code != 0 {
		t.Fatalf("empty selection failed: %q", diagnostics.String())
	}
	if output.String() != "{\"files\":{}}\n" || diagnostics.Len() != 0 {
		t.Fatalf("unexpected output: %q, %q", output.String(), diagnostics.String())
	}
}

func TestManifestRejectsMalformedAndDuplicateJSON(t *testing.T) {
	tests := []string{
		"", "null", "[]", "{}", `{"files":null}`, `{"files":[]}`, `{"Files":{}}`,
		`{"files":{},"extra":true}`, `{"extra":{},"files":{}}`,
		`{"files":{},"files":{}}`, `{"files":{}} {}`, `{"files":{}} false`,
		`{"files":{"internal/a.go":null}}`, `{"files":{"internal/a.go":1}}`,
		`{"files":{"internal/a.go":{}}}`, `{"files":{"internal/a.go":[]}}`,
		`{"files":{"internal/a.go":true}}`, `{"files":{"internal/a.go":""}}`,
		`{"files":{"internal/a.go":"example/internal","internal/a.go":"other/internal"}}`,
		`{"files":{"internal/a.go":"example/internal","\u0069nternal/a.go":"example/internal"}}`,
		`{"files":{"internal/a.go":"example/internal",}}`,
		`{"files":{"internal/a.go":"example/internal"}`,
		"{\"files\":{\"internal/\xff.go\":\"example/internal\"}}",
	}
	for index, input := range tests {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			requireFailure(t, t.TempDir(), strings.NewReader(input))
		})
	}
}

func TestManifestRejectsInvalidPaths(t *testing.T) {
	names := []string{
		"", "a.go", "other/a.go", "third_party/a.go", "/internal/a.go",
		"../internal/a.go", "internal/../cmd/a.go", "./internal/a.go",
		"internal//a.go", "internal/./a.go", "internal/a.go/",
		`internal\a.go`, "internal/a_test.go", "internal/a.Go", "internal/a.txt",
		"internal/a\n.go", "internal/a\x00.go", "internal/a b.go", "internal/a\u007f.go",
		"internal/C:/a.go",
	}
	for index, name := range names {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			requireFailure(t, t.TempDir(), bytes.NewReader(request(t,
				map[string]string{name: "example/internal"})))
		})
	}
	packagePaths := []string{"", ".", "..", "/example/internal", "example//internal",
		"example/../internal", `example\internal`, "example internal", "example/internal\n"}
	for index, packagePath := range packagePaths {
		t.Run(fmt.Sprintf("package-%d", index), func(t *testing.T) {
			requireFailure(t, t.TempDir(), bytes.NewReader(request(t,
				map[string]string{"internal/a.go": packagePath})))
		})
	}
}

func TestManifestEnforcesInputLimits(t *testing.T) {
	for _, size := range []int{maxInputBytes, maxInputBytes + 1} {
		t.Run(fmt.Sprintf("bytes-%d", size), func(t *testing.T) {
			raw := []byte(`{"files":{}}`)
			raw = append(raw, bytes.Repeat([]byte(" "), size-len(raw))...)
			_, err := readFiles(bytes.NewReader(raw))
			if (err != nil) != (size > maxInputBytes) {
				t.Fatalf("size %d: error = %v", size, err)
			}
			if size > maxInputBytes {
				requireFailure(t, t.TempDir(), bytes.NewReader(raw))
			}
		})
	}
	for _, count := range []int{maxFiles, maxFiles + 1} {
		t.Run(fmt.Sprintf("files-%d", count), func(t *testing.T) {
			files := make(map[string]string, count)
			for index := range count {
				files[fmt.Sprintf("tools/sample/file%04d.go", index)] = "example/tools/sample"
			}
			raw := request(t, files)
			actual, err := readFiles(bytes.NewReader(raw))
			if count > maxFiles {
				if err == nil {
					t.Fatal("accepted too many files")
				}
				requireFailure(t, t.TempDir(), bytes.NewReader(raw))
			} else if err != nil || !reflect.DeepEqual(actual, files) {
				t.Fatalf("accepted boundary mismatch: error = %v, files = %d", err, len(actual))
			}
		})
	}
}

func TestManifestRejectsStaleNonregularAndMalformedSources(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		requireFailure(t, t.TempDir(), bytes.NewReader(request(t,
			map[string]string{"internal/stale.go": "example/internal"})))
	})
	t.Run("directory", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "internal/directory.go"), 0o700); err != nil {
			t.Fatal(err)
		}
		requireFailure(t, root, bytes.NewReader(request(t,
			map[string]string{"internal/directory.go": "example/internal"})))
	})
	t.Run("parse error after valid function", func(t *testing.T) {
		root := t.TempDir()
		writeSource(t, root, "internal/valid.go", "package sample\nfunc Valid() {}\n")
		writeSource(t, root, "internal/PRIVATE_PATH.go",
			"package sample\nfunc Valid() {}\nPRIVATE_SOURCE_CONTENT not valid Go\n")
		requireFailure(t, root, bytes.NewReader(request(t, map[string]string{
			"internal/valid.go":        "example/internal",
			"internal/PRIVATE_PATH.go": "example/internal",
		})))
	})
	t.Run("missing root", func(t *testing.T) {
		requireFailure(t, filepath.Join(t.TempDir(), "missing"), strings.NewReader(`{"files":{}}`))
	})
}

func TestManifestRejectsSymlinks(t *testing.T) {
	for _, outside := range []bool{false, true} {
		for _, directory := range []bool{false, true} {
			t.Run(fmt.Sprintf("outside-%t-directory-%t", outside, directory), func(t *testing.T) {
				root := t.TempDir()
				targetRoot := root
				if outside {
					targetRoot = t.TempDir()
				}
				writeSource(t, targetRoot, "internal/target/a.go", "package sample\nfunc Empty() {}\n")
				link := filepath.Join(root, "tools/link.go")
				target := filepath.Join(targetRoot, "internal/target/a.go")
				name := "tools/link.go"
				if directory {
					link = filepath.Join(root, "tools/link")
					target = filepath.Join(targetRoot, "internal/target")
					name = "tools/link/a.go"
				}
				if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
					t.Fatal(err)
				}
				if !outside {
					var err error
					target, err = filepath.Rel(filepath.Dir(link), target)
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(target, link); err != nil {
					t.Fatal(err)
				}
				requireFailure(t, root, bytes.NewReader(request(t,
					map[string]string{name: "example/tools/link"})))
			})
		}
	}
	t.Run("owned root", func(t *testing.T) {
		root := t.TempDir()
		targetRoot := t.TempDir()
		writeSource(t, targetRoot, "a.go", "package sample\nfunc Empty() {}\n")
		if err := os.Symlink(targetRoot, filepath.Join(root, "internal")); err != nil {
			t.Fatal(err)
		}
		requireFailure(t, root, bytes.NewReader(request(t,
			map[string]string{"internal/a.go": "example/internal"})))
	})
}

type failedReader struct{}

func (failedReader) Read([]byte) (int, error) {
	return 0, errors.New("PRIVATE_READER_ERROR")
}

func TestManifestRedactsInputReadErrors(t *testing.T) {
	requireFailure(t, t.TempDir(), failedReader{})
}
