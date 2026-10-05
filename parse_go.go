// Go source parser built on the stdlib go/ast.

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

func parseGo(path, source string) *CodeModule {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, source, parser.ParseComments)
	if err != nil {
		return nil
	}

	mod := &CodeModule{
		Language: "go",
		FilePath: path,
		Package:  f.Name.Name,
	}

	// Package doc
	if f.Doc != nil {
		mod.Docstring = f.Doc.Text()
	}

	// Imports
	for _, imp := range f.Imports {
		impPath := strings.Trim(imp.Path.Value, `"`)
		mod.Imports = append(mod.Imports, impPath)
	}

	// Walk declarations
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					ct := CodeType{
						Name:       s.Name.Name,
						IsExported: ast.IsExported(s.Name.Name),
					}
					if d.Doc != nil {
						ct.Docstring = d.Doc.Text()
					}
					switch st := s.Type.(type) {
					case *ast.StructType:
						ct.Kind = "struct"
						if st.Fields != nil {
							for _, field := range st.Fields.List {
								for _, name := range field.Names {
									ct.Fields = append(ct.Fields, name.Name)
								}
							}
						}
					case *ast.InterfaceType:
						ct.Kind = "interface"
						if st.Methods != nil {
							for _, m := range st.Methods.List {
								for _, name := range m.Names {
									ct.Methods = append(ct.Methods, CodeFunc{
										Name:       name.Name,
										IsExported: ast.IsExported(name.Name),
									})
								}
							}
						}
					default:
						ct.Kind = "type"
					}
					mod.Types = append(mod.Types, ct)

				case *ast.ValueSpec:
					for _, name := range s.Names {
						if ast.IsExported(name.Name) {
							mod.Constants = append(mod.Constants, name.Name)
						}
					}
				}
			}

		case *ast.FuncDecl:
			fn := CodeFunc{
				Name:       d.Name.Name,
				IsExported: ast.IsExported(d.Name.Name),
			}
			if d.Doc != nil {
				fn.Docstring = d.Doc.Text()
			}
			// Args
			if d.Type.Params != nil {
				for _, p := range d.Type.Params.List {
					for _, name := range p.Names {
						fn.Args = append(fn.Args, name.Name)
					}
				}
			}
			// Returns
			if d.Type.Results != nil {
				var rets []string
				for _, r := range d.Type.Results.List {
					if len(r.Names) > 0 {
						for _, name := range r.Names {
							rets = append(rets, name.Name)
						}
					} else {
						rets = append(rets, "...")
					}
				}
				fn.Returns = strings.Join(rets, ", ")
			}
			// Method receiver → attach to type
			if d.Recv != nil && len(d.Recv.List) > 0 {
				recvType := exprName(d.Recv.List[0].Type)
				attached := false
				for i := range mod.Types {
					if mod.Types[i].Name == recvType {
						mod.Types[i].Methods = append(mod.Types[i].Methods, fn)
						attached = true
						break
					}
				}
				if !attached {
					// Type not yet seen — create placeholder
					mod.Types = append(mod.Types, CodeType{
						Name:    recvType,
						Kind:    "struct",
						Methods: []CodeFunc{fn},
					})
				}
			} else {
				mod.Functions = append(mod.Functions, fn)
			}
		}
	}

	return mod
}

// exprName extracts the type name from a receiver expression (handles *T and T).
func exprName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return exprName(e.X)
	default:
		return ""
	}
}
