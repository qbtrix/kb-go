// TypeScript/JavaScript source parser (regex-based).

package main

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	tsClassRe     = regexp.MustCompile(`(?m)^(?:export\s+)?(?:abstract\s+)?class\s+(\w+)(?:\s+extends\s+(\w+))?(?:\s+implements\s+([^{]+))?\s*\{`)
	tsInterfaceRe = regexp.MustCompile(`(?m)^(?:export\s+)?interface\s+(\w+)(?:\s+extends\s+([^{]+))?\s*\{`)
	tsFuncRe      = regexp.MustCompile(`(?m)^(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*(?:<[^>]+>)?\s*\(([^)]*)\)(?:\s*:\s*([^\s{]+))?\s*\{`)
	tsArrowRe     = regexp.MustCompile(`(?m)^(?:export\s+)?(?:const|let)\s+(\w+)\s*=\s*(?:async\s+)?\([^)]*\)(?:\s*:\s*[^\s=]+)?\s*=>`)
	tsImportRe    = regexp.MustCompile(`(?m)^import\s+(?:\{([^}]+)\}|(\w+))\s+from\s+['"]([^'"]+)['"]`)
	tsTypeRe      = regexp.MustCompile(`(?m)^(?:export\s+)?type\s+(\w+)(?:<[^>]+>)?\s*=`)
	tsEnumRe      = regexp.MustCompile(`(?m)^(?:export\s+)?(?:const\s+)?enum\s+(\w+)\s*\{`)
)

func parseTypeScript(path, source, lang string) *CodeModule {
	mod := &CodeModule{
		Language: lang,
		FilePath: path,
	}

	// Imports
	for _, match := range tsImportRe.FindAllStringSubmatch(source, -1) {
		mod.Imports = append(mod.Imports, match[3])
	}

	// Classes
	for _, match := range tsClassRe.FindAllStringSubmatch(source, -1) {
		ct := CodeType{
			Name:       match[1],
			Kind:       "class",
			IsExported: strings.Contains(match[0], "export"),
		}
		if match[2] != "" {
			ct.Bases = append(ct.Bases, strings.TrimSpace(match[2]))
		}
		if match[3] != "" {
			for _, impl := range strings.Split(match[3], ",") {
				ct.Bases = append(ct.Bases, strings.TrimSpace(impl))
			}
		}
		mod.Types = append(mod.Types, ct)
	}

	// Interfaces
	for _, match := range tsInterfaceRe.FindAllStringSubmatch(source, -1) {
		ct := CodeType{
			Name:       match[1],
			Kind:       "interface",
			IsExported: strings.Contains(match[0], "export"),
		}
		if match[2] != "" {
			for _, ext := range strings.Split(match[2], ",") {
				ct.Bases = append(ct.Bases, strings.TrimSpace(ext))
			}
		}
		mod.Types = append(mod.Types, ct)
	}

	// Type aliases
	for _, match := range tsTypeRe.FindAllStringSubmatch(source, -1) {
		mod.Types = append(mod.Types, CodeType{
			Name:       match[1],
			Kind:       "type",
			IsExported: strings.Contains(match[0], "export"),
		})
	}

	// Enums
	for _, match := range tsEnumRe.FindAllStringSubmatch(source, -1) {
		mod.Types = append(mod.Types, CodeType{
			Name:       match[1],
			Kind:       "enum",
			IsExported: strings.Contains(match[0], "export"),
		})
	}

	// Functions
	for _, match := range tsFuncRe.FindAllStringSubmatch(source, -1) {
		fn := CodeFunc{
			Name:       match[1],
			IsAsync:    strings.Contains(match[0], "async"),
			IsExported: strings.Contains(match[0], "export"),
		}
		if match[2] != "" {
			for _, arg := range strings.Split(match[2], ",") {
				arg = strings.TrimSpace(arg)
				arg = strings.SplitN(arg, ":", 2)[0]
				arg = strings.SplitN(arg, "=", 2)[0]
				arg = strings.TrimSpace(arg)
				if arg != "" {
					fn.Args = append(fn.Args, arg)
				}
			}
		}
		if match[3] != "" {
			fn.Returns = match[3]
		}
		mod.Functions = append(mod.Functions, fn)
	}

	// Arrow functions (exported const)
	for _, match := range tsArrowRe.FindAllStringSubmatch(source, -1) {
		fn := CodeFunc{
			Name:       match[1],
			IsAsync:    strings.Contains(match[0], "async"),
			IsExported: strings.Contains(match[0], "export"),
		}
		mod.Functions = append(mod.Functions, fn)
	}

	return mod
}

// formatCodeContext produces a structured summary for the LLM prompt.
func formatCodeContext(mod *CodeModule) string {
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
				fmt.Fprintf(&sb, "    doc: %s\n", truncate(t.Docstring, 100))
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
				fmt.Fprintf(&sb, "    doc: %s\n", truncate(fn.Docstring, 100))
			}
		}
	}

	if len(mod.Constants) > 0 {
		fmt.Fprintf(&sb, "\nConstants: %s\n", strings.Join(mod.Constants, ", "))
	}

	return sb.String()
}
