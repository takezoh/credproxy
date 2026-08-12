package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConsumerCommandsStayOutsideCredproxy is the fitness function for
// design-credential-broker INV-008. Provider helper commands acquire credentials;
// they do not admit or execute the operation that consumes them.
func TestConsumerCommandsStayOutsideCredproxy(t *testing.T) {
	root := filepath.Clean("..")
	forbidden := []string{
		"OperationRunner", "OperationArgument", "ExecutablePaths",
		"BindingRevision", "MaxRuntime", "/v1/operations/",
		"closed operation", "toml:\"operation\"",
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if rel == ".git" || rel == "architecture" || strings.HasPrefix(rel, "docs") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, word := range forbidden {
			if strings.Contains(string(data), word) {
				t.Errorf("%s owns forbidden consumer-operation vocabulary %q", rel, word)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Proxy core and daemon assembly may not start consumer processes. Provider
	// implementations and cmd/credproxy's generic caller-selected helper are
	// deliberately outside this check.
	for _, dir := range []string{"credproxy", "cmd/credproxyd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				rel, _ := filepath.Rel(root, path)
				if strings.HasPrefix(rel, "cmd/credproxyd/providers") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imp := range file.Imports {
				if imp.Path.Value == `"os/exec"` {
					t.Errorf("%s imports os/exec inside proxy/daemon ownership", path)
				}
			}
			ast.Inspect(file, func(ast.Node) bool { return true })
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
