// Package parse extracts the structure of a source file (package, imports,
// types, functions, constants, docstrings) into a Module that feeds the
// compile prompt. Code dispatches on the file extension: Go through the
// stdlib go/ast, Python and TypeScript/JavaScript through regexes.
// FormatContext renders a Module as the prompt's "AST-extracted structure"
// block. Parsing never fails: unparseable input yields a sparse Module.
package parse

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/qbtrix/kb-go/internal/textutil"
)

// Module holds extracted structure from a source file.
type Module struct {
	Language  string     `json:"language"` // go, python, typescript
	FilePath  string     `json:"file_path"`
	Package   string     `json:"package,omitempty"`
	Imports   []string   `json:"imports,omitempty"`
	Types     []CodeType `json:"types,omitempty"` // structs, classes, interfaces
	Functions []CodeFunc `json:"functions,omitempty"`
	Constants []string   `json:"constants,omitempty"`
	Docstring string     `json:"docstring,omitempty"` // module/package doc
}

type CodeType struct {
	Name       string     `json:"name"`
	Kind       string     `json:"kind"` // struct, class, interface, type
	Bases      []string   `json:"bases,omitempty"`
	Methods    []CodeFunc `json:"methods,omitempty"`
	Fields     []string   `json:"fields,omitempty"`
	Docstring  string     `json:"docstring,omitempty"`
	IsExported bool       `json:"is_exported,omitempty"`
}

type CodeFunc struct {
	Name       string   `json:"name"`
	Args       []string `json:"args,omitempty"`
	Returns    string   `json:"returns,omitempty"`
	IsAsync    bool     `json:"is_async,omitempty"`
	IsExported bool     `json:"is_exported,omitempty"`
	Docstring  string   `json:"docstring,omitempty"`
}

// DetectLanguage returns the language from file extension.
func DetectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	default:
		return ""
	}
}

// Code dispatches to the right parser based on language.
func Code(path, source string) *Module {
	lang := DetectLanguage(path)
	switch lang {
	case "go":
		return parseGo(path, source)
	case "python":
		return parsePython(path, source)
	case "typescript", "javascript":
		return parseTypeScript(path, source, lang)
	default:
		return nil
	}
}

// FormatContext produces a structured summary for the LLM prompt.
func FormatContext(mod *Module) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Language: %s\n", mod.Language)
	if mod.Package != "" {
		fmt.Fprintf(&sb, "Package: %s\n", mod.Package)
	}
	if mod.Docstring != "" {
		fmt.Fprintf(&sb, "Module doc: %s\n", mod.Docstring)
	}

	if len(mod.Imports) > 0 {
		fmt.Fprintf(&sb, "\nImports: %s\n", strings.Join(mod.Imports, ", "))
	}

	if len(mod.Types) > 0 {
		sb.WriteString("\nTypes:\n")
		for _, t := range mod.Types {
			exported := ""
			if t.IsExported {
				exported = " (exported)"
			}
			bases := ""
			if len(t.Bases) > 0 {
				bases = " extends " + strings.Join(t.Bases, ", ")
			}
			fmt.Fprintf(&sb, "  %s %s%s%s\n", t.Kind, t.Name, bases, exported)
			if t.Docstring != "" {
				fmt.Fprintf(&sb, "    doc: %s\n", textutil.Truncate(t.Docstring, 100))
			}
			for _, f := range t.Fields {
				fmt.Fprintf(&sb, "    field: %s\n", f)
			}
			for _, m := range t.Methods {
				async := ""
				if m.IsAsync {
					async = "async "
				}
				fmt.Fprintf(&sb, "    %smethod: %s(%s)", async, m.Name, strings.Join(m.Args, ", "))
				if m.Returns != "" {
					fmt.Fprintf(&sb, " -> %s", m.Returns)
				}
				sb.WriteString("\n")
			}
		}
	}

	if len(mod.Functions) > 0 {
		sb.WriteString("\nFunctions:\n")
		for _, fn := range mod.Functions {
			async := ""
			if fn.IsAsync {
				async = "async "
			}
			exported := ""
			if fn.IsExported {
				exported = " (exported)"
			}
			fmt.Fprintf(&sb, "  %s%s(%s)", async, fn.Name, strings.Join(fn.Args, ", "))
			if fn.Returns != "" {
				fmt.Fprintf(&sb, " -> %s", fn.Returns)
			}
			fmt.Fprintf(&sb, "%s\n", exported)
			if fn.Docstring != "" {
				fmt.Fprintf(&sb, "    doc: %s\n", textutil.Truncate(fn.Docstring, 100))
			}
		}
	}

	if len(mod.Constants) > 0 {
		fmt.Fprintf(&sb, "\nConstants: %s\n", strings.Join(mod.Constants, ", "))
	}

	return sb.String()
}
