//go:build ignore

// boundarycheck flags exported API symbols in pkg/... whose signatures or
// exported struct fields reference an internal/ package. Run:
//
//	go run testdata/boundarycheck.go
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	fset := token.NewFileSet()
	leaks := 0
	err := filepath.WalkDir("pkg", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		entries, _ := os.ReadDir(p)
		var files []*ast.File
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			f, perr := parser.ParseFile(fset, filepath.Join(p, e.Name()), nil, 0)
			if perr != nil {
				return perr
			}
			files = append(files, f)
		}
		for _, f := range files {
			internal := map[string]string{} // local name -> full internal path
			for _, imp := range f.Imports {
				ip, _ := strconv.Unquote(imp.Path.Value)
				if strings.Contains(ip, "/internal/") {
					name := path.Base(ip)
					if imp.Name != nil {
						name = imp.Name.Name
					}
					internal[name] = ip
				}
			}
			if len(internal) == 0 {
				continue
			}
			report := func(kind, name string, node ast.Node) {
				if node == nil {
					return
				}
				ast.Inspect(node, func(n ast.Node) bool {
					sel, ok := n.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					id, ok := sel.X.(*ast.Ident)
					if !ok || internal[id.Name] == "" {
						return true
					}
					fmt.Printf("%s: %s %s references %s.%s (%s)\n",
						fset.Position(sel.Pos()), kind, name, id.Name, sel.Sel.Name, internal[id.Name])
					leaks++
					return true
				})
			}
			// reportType flags only exported struct fields and interface methods,
			// so unexported fields of internal types do not count.
			reportType := func(name string, t ast.Expr) {
				switch st := t.(type) {
				case *ast.StructType:
					for _, field := range st.Fields.List {
						if !fieldExported(field) {
							continue
						}
						report("type", name, field.Type)
					}
				case *ast.InterfaceType:
					report("interface", name, st)
				default:
					report("type", name, t)
				}
			}
			for _, decl := range f.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					if !exportedFunc(d) {
						continue
					}
					if d.Recv != nil {
						report("method", d.Name.Name, d.Recv)
					}
					if d.Type.Params != nil {
						report("func", d.Name.Name, d.Type.Params)
					}
					if d.Type.Results != nil {
						report("func", d.Name.Name, d.Type.Results)
					}
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						switch s := spec.(type) {
						case *ast.TypeSpec:
							if ast.IsExported(s.Name.Name) {
								reportType(s.Name.Name, s.Type)
							}
						case *ast.ValueSpec:
							for _, n := range s.Names {
								if ast.IsExported(n.Name) {
									report("var", n.Name, s.Type)
								}
							}
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if leaks > 0 {
		fmt.Printf("\n%d leak(s)\n", leaks)
		os.Exit(1)
	}
	fmt.Println("boundary OK: no exported pkg/ symbol references internal/")
}

func fieldExported(f *ast.Field) bool {
	if len(f.Names) == 0 {
		return ast.IsExported(baseTypeName(f.Type))
	}
	for _, n := range f.Names {
		if ast.IsExported(n.Name) {
			return true
		}
	}
	return false
}

func exportedFunc(d *ast.FuncDecl) bool {
	if !ast.IsExported(d.Name.Name) {
		return false
	}
	if d.Recv == nil {
		return true
	}
	// A method is public only if its receiver type is exported too.
	for _, f := range d.Recv.List {
		if ast.IsExported(baseTypeName(f.Type)) {
			return true
		}
	}
	return false
}

func baseTypeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return baseTypeName(t.X)
	case *ast.IndexExpr:
		return baseTypeName(t.X)
	case *ast.IndexListExpr:
		return baseTypeName(t.X)
	}
	return ""
}
