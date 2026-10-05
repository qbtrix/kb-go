// Python source parser (regex-based).

package parse

import (
	"regexp"
	"strings"
)

var (
	pyClassRe     = regexp.MustCompile(`(?m)^class\s+(\w+)\s*(?:\(([^)]*)\))?\s*:`)
	pyFuncRe      = regexp.MustCompile(`(?m)^(\s*)(async\s+)?def\s+(\w+)\s*\(([^)]*)\)(?:\s*->\s*([^\s:]+))?\s*:`)
	pyImportRe    = regexp.MustCompile(`(?m)^(?:from\s+(\S+)\s+)?import\s+(.+)$`)
	pyDocstringRe = regexp.MustCompile(`(?m)^\s*"""((?s:.*?))"""`)
	pyConstRe     = regexp.MustCompile(`(?m)^([A-Z][A-Z0-9_]+)\s*=`)
)

func parsePython(path, source string) *Module {
	mod := &Module{
		Language: "python",
		FilePath: path,
	}

	lines := strings.Split(source, "\n")

	// Module docstring (first triple-quoted string)
	if match := pyDocstringRe.FindStringSubmatch(source); match != nil {
		// Only count as module doc if it starts near the top (within first 5 non-empty lines)
		idx := strings.Index(source, match[0])
		prefix := source[:idx]
		nonEmpty := 0
		for _, l := range strings.Split(prefix, "\n") {
			if strings.TrimSpace(l) != "" && !strings.HasPrefix(strings.TrimSpace(l), "#") {
				nonEmpty++
			}
		}
		if nonEmpty == 0 {
			mod.Docstring = strings.TrimSpace(match[1])
		}
	}

	// Imports
	for _, match := range pyImportRe.FindAllStringSubmatch(source, -1) {
		if match[1] != "" {
			mod.Imports = append(mod.Imports, match[1])
		} else {
			for _, imp := range strings.Split(match[2], ",") {
				imp = strings.TrimSpace(imp)
				if imp != "" {
					mod.Imports = append(mod.Imports, imp)
				}
			}
		}
	}

	// Constants
	for _, match := range pyConstRe.FindAllStringSubmatch(source, -1) {
		mod.Constants = append(mod.Constants, match[1])
	}

	// Classes and their methods
	classLocs := pyClassRe.FindAllStringSubmatchIndex(source, -1)
	for i, loc := range classLocs {
		match := pyClassRe.FindStringSubmatch(source[loc[0]:loc[1]])
		ct := CodeType{
			Name: match[1],
			Kind: "class",
		}
		if match[2] != "" {
			for _, base := range strings.Split(match[2], ",") {
				base = strings.TrimSpace(base)
				if base != "" {
					ct.Bases = append(ct.Bases, base)
				}
			}
		}

		// Find class body (until next class or EOF)
		start := loc[1]
		end := len(source)
		if i+1 < len(classLocs) {
			end = classLocs[i+1][0]
		}
		classBody := source[start:end]

		// Class docstring
		if docMatch := pyDocstringRe.FindStringSubmatch(classBody); docMatch != nil {
			ct.Docstring = strings.TrimSpace(docMatch[1])
		}

		// Methods within class
		for _, fMatch := range pyFuncRe.FindAllStringSubmatch(classBody, -1) {
			indent := fMatch[1]
			if len(indent) < 4 { // not indented enough to be a method
				continue
			}
			fn := CodeFunc{
				Name:    fMatch[3],
				IsAsync: fMatch[2] != "",
			}
			// Args (skip self/cls)
			args := strings.Split(fMatch[4], ",")
			for _, arg := range args {
				arg = strings.TrimSpace(arg)
				arg = strings.SplitN(arg, ":", 2)[0]
				arg = strings.SplitN(arg, "=", 2)[0]
				arg = strings.TrimSpace(arg)
				if arg != "" && arg != "self" && arg != "cls" {
					fn.Args = append(fn.Args, arg)
				}
			}
			if fMatch[5] != "" {
				fn.Returns = fMatch[5]
			}
			ct.Methods = append(ct.Methods, fn)
		}

		mod.Types = append(mod.Types, ct)
	}

	// Top-level functions (not indented)
	for _, lineNum := range findLines(lines, pyFuncRe) {
		line := lines[lineNum]
		fMatch := pyFuncRe.FindStringSubmatch(line)
		if fMatch == nil || len(fMatch[1]) > 0 { // skip indented (methods)
			continue
		}
		fn := CodeFunc{
			Name:    fMatch[3],
			IsAsync: fMatch[2] != "",
		}
		args := strings.Split(fMatch[4], ",")
		for _, arg := range args {
			arg = strings.TrimSpace(arg)
			arg = strings.SplitN(arg, ":", 2)[0]
			arg = strings.SplitN(arg, "=", 2)[0]
			arg = strings.TrimSpace(arg)
			if arg != "" {
				fn.Args = append(fn.Args, arg)
			}
		}
		if fMatch[5] != "" {
			fn.Returns = fMatch[5]
		}
		// Check for docstring on next line
		if lineNum+1 < len(lines) {
			nextLine := strings.TrimSpace(lines[lineNum+1])
			if strings.HasPrefix(nextLine, `"""`) {
				doc := strings.Trim(nextLine, `" `)
				fn.Docstring = doc
			}
		}
		fn.IsExported = !strings.HasPrefix(fn.Name, "_")
		mod.Functions = append(mod.Functions, fn)
	}

	return mod
}

func findLines(lines []string, re *regexp.Regexp) []int {
	var result []int
	for i, line := range lines {
		if re.MatchString(line) {
			result = append(result, i)
		}
	}
	return result
}
