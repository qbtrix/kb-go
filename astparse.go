// Language-agnostic AST extraction: the CodeModule shape every parser fills,
// and dispatch from a file extension to the right parser.

package main

import (
	"path/filepath"
	"strings"
)

// CodeModule holds extracted structure from a source file.
type CodeModule struct {
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

// detectLanguage returns the language from file extension.
func detectLanguage(path string) string {
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

// parseCode dispatches to the right parser based on language.
func parseCode(path, source string) *CodeModule {
	lang := detectLanguage(path)
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
