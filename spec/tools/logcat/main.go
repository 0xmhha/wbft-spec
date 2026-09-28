// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

// logcat: extracts "WBFT: " log statements (with fields and inherited logger
// context) and error definitions/uses from consensus/wbft/** of go-stablenet.
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// root is the go-stablenet checkout (740526d03), taken from $GO_STABLENET.
var root = os.Getenv("GO_STABLENET")
var fset = token.NewFileSet()

type fileInfo struct {
	rel  string
	file *ast.File
}

func src(n ast.Node) string {
	var b bytes.Buffer
	printer.Fprint(&b, fset, n)
	s := strings.Join(strings.Fields(b.String()), " ")
	return s
}

var levels = map[string]bool{"Trace": true, "Debug": true, "Info": true, "Warn": true, "Error": true, "Crit": true}

func strLit(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			s, err := strconv.Unquote(v.Value)
			return s, err == nil
		}
	case *ast.CallExpr:
		// fmt.Sprintf("...", ...)
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Sprintf" && len(v.Args) > 0 {
			if s, ok := strLit(v.Args[0]); ok {
				return "fmt: " + s, true
			}
		}
	}
	return "", false
}

// pairs renders key/value argument pairs.
func pairs(args []ast.Expr) []string {
	var out []string
	for i := 0; i+1 < len(args); i += 2 {
		k := src(args[i])
		if s, ok := strLit(args[i]); ok {
			k = s
		}
		out = append(out, fmt.Sprintf("%s=%s", k, src(args[i+1])))
	}
	if len(args)%2 == 1 {
		out = append(out, "(odd)"+src(args[len(args)-1]))
	}
	return out
}

func keysOnly(args []ast.Expr) []string {
	var out []string
	for i := 0; i+1 < len(args); i += 2 {
		k := src(args[i])
		if s, ok := strLit(args[i]); ok {
			k = s
		}
		out = append(out, k)
	}
	return out
}

// context resolution
type funcScope struct {
	body *ast.BlockStmt
	fn   string
}

func lastAssign(fs *funcScope, name string, before token.Pos) ast.Expr {
	var best ast.Expr
	var bestPos token.Pos
	ast.Inspect(fs.body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || as.End() > before {
			return true
		}
		for i, l := range as.Lhs {
			if id, ok := l.(*ast.Ident); ok && id.Name == name && i < len(as.Rhs) {
				if as.Pos() > bestPos {
					best, bestPos = as.Rhs[i], as.Pos()
				}
			}
		}
		return true
	})
	// var x = ...
	ast.Inspect(fs.body, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || vs.End() > before {
			return true
		}
		for i, id := range vs.Names {
			if id.Name == name && i < len(vs.Values) && vs.Pos() > bestPos {
				best, bestPos = vs.Values[i], vs.Pos()
			}
		}
		return true
	})
	return best
}

func ctxOf(fs *funcScope, e ast.Expr, depth int) []string {
	if depth > 8 || e == nil {
		return nil
	}
	switch v := e.(type) {
	case *ast.Ident:
		if v.Name == "log" {
			return nil
		}
		return ctxOf(fs, lastAssign(fs, v.Name, v.Pos()), depth+1)
	case *ast.SelectorExpr:
		s := src(v)
		switch s {
		case "c.logger":
			return []string{"address"}
		case "sb.logger", "api.logger":
			return nil
		}
		return []string{"<" + s + ">"}
	case *ast.CallExpr:
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok {
			switch sel.Sel.Name {
			case "New":
				if src(sel.X) == "log" {
					return keysOnly(v.Args)
				}
				return append(ctxOf(fs, sel.X, depth+1), keysOnly(v.Args)...)
			case "currentLogger":
				out := []string{"address", "current.round", "current.sequence"}
				if len(v.Args) > 0 && src(v.Args[0]) == "true" {
					out = append(out, "state")
				}
				if len(v.Args) > 1 && src(v.Args[1]) != "nil" {
					out = append(out, "msg.code", "msg.source", "msg.round", "msg.sequence")
				}
				return out
			}
		}
		if id, ok := v.Fun.(*ast.Ident); ok && id.Name == "withMsg" && len(v.Args) == 2 {
			return append(ctxOf(fs, v.Args[0], depth+1), "msg.code", "msg.source", "msg.round", "msg.sequence")
		}
		return []string{"<" + src(v) + ">"}
	}
	return []string{"<" + src(e) + ">"}
}

type logRow struct {
	loc, level, msg, recv string
	fields, ctx          []string
}

func main() {
	if root == "" {
		fmt.Fprintln(os.Stderr, "logcat: set GO_STABLENET to the go-stablenet checkout (usage: GO_STABLENET=<path> go run . [logs|errors])")
		os.Exit(2)
	}
	var files []fileInfo
	filepath.Walk(filepath.Join(root, "consensus/wbft"), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		if strings.Contains(p, "/testutils/") || strings.HasSuffix(p, "testutil.go") {
			// test helpers: still scan, flag later
		}
		f, err := parser.ParseFile(fset, p, nil, parser.ParseComments)
		if err != nil {
			panic(err)
		}
		rel, _ := filepath.Rel(root, p)
		files = append(files, fileInfo{rel, f})
		return nil
	})
	mode := "logs"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	if mode == "logs" {
		var rows []logRow
		for _, fi := range files {
			for _, d := range fi.file.Decls {
				fd, ok := d.(*ast.FuncDecl)
				var body *ast.BlockStmt
				name := ""
				if ok {
					body = fd.Body
					name = fd.Name.Name
				}
				if body == nil {
					// package-level: skip
					continue
				}
				fs := &funcScope{body: body, fn: name}
				ast.Inspect(body, func(n ast.Node) bool {
					ce, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := ce.Fun.(*ast.SelectorExpr)
					if !ok || !levels[sel.Sel.Name] || len(ce.Args) == 0 {
						return true
					}
					m, ok := strLit(ce.Args[0])
					if !ok || !(strings.HasPrefix(m, "WBFT: ") || strings.HasPrefix(m, "fmt: WBFT: ")) {
						return true
					}
					p := fset.Position(ce.Pos())
					rows = append(rows, logRow{
						loc:    fmt.Sprintf("%s:%d", strings.TrimPrefix(fi.rel, "consensus/wbft/"), p.Line),
						level:  sel.Sel.Name,
						msg:    m,
						recv:   src(sel.X),
						fields: argsFields(ce),
						ctx:    ctxOf(fs, sel.X, 0),
					})
					return true
				})
			}
		}
		sort.Slice(rows, func(i, j int) bool {
			a, b := rows[i].loc, rows[j].loc
			fa, la := splitLoc(a)
			fb, lb := splitLoc(b)
			if fa != fb {
				return fa < fb
			}
			return la < lb
		})
		for i, r := range rows {
			msg := strings.ReplaceAll(r.msg, "|", "\\|")
			f := strings.ReplaceAll(strings.Join(r.fields, ", "), "|", "\\|")
			c := strings.ReplaceAll(strings.Join(dedup(r.ctx), ", "), "|", "\\|")
			if f == "" {
				f = "—"
			}
			if c == "" {
				c = "—"
			}
			fmt.Printf("| L%03d | `%s` | %s | %s | `%s` | %s |\n", i+1, r.loc, r.level, msg, f, c)
		}
		fmt.Fprintf(os.Stderr, "total=%d\n", len(rows))
		return
	}
	if mode == "errors" {
		// definitions: package-level var X = errors.New(...)
		type def struct {
			pkg, name, msg, loc string
		}
		var defs []def
		for _, fi := range files {
			for _, d := range fi.file.Decls {
				gd, ok := d.(*ast.GenDecl)
				if !ok || gd.Tok != token.VAR {
					continue
				}
				for _, sp := range gd.Specs {
					vs := sp.(*ast.ValueSpec)
					for i, id := range vs.Names {
						if i >= len(vs.Values) {
							continue
						}
						if ce, ok := vs.Values[i].(*ast.CallExpr); ok {
							if s := src(ce.Fun); s == "errors.New" {
								m, _ := strLit(ce.Args[0])
								defs = append(defs, def{fi.file.Name.Name, id.Name, m, fmt.Sprintf("%s:%d", fi.rel, fset.Position(id.Pos()).Line)})
							}
						}
					}
				}
			}
		}
		// uses
		uses := map[string][]string{}
		inline := []string{}
		for _, fi := range files {
			for _, d := range fi.file.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				fname := fd.Name.Name
				if fd.Recv != nil && len(fd.Recv.List) > 0 {
					fname = src(fd.Recv.List[0].Type) + "." + fname
				}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					switch v := n.(type) {
					case *ast.Ident:
						for _, df := range defs {
							if v.Name == df.name {
								uses[df.name] = append(uses[df.name], fmt.Sprintf("%s:%d (%s)", strings.TrimPrefix(fi.rel, "consensus/wbft/"), fset.Position(v.Pos()).Line, fname))
							}
						}
					case *ast.CallExpr:
						s := src(v.Fun)
						if s == "errors.New" || s == "fmt.Errorf" {
							m := ""
							if len(v.Args) > 0 {
								m, _ = strLit(v.Args[0])
							}
							inline = append(inline, fmt.Sprintf("%s:%d | %s | %s | %s", strings.TrimPrefix(fi.rel, "consensus/wbft/"), fset.Position(v.Pos()).Line, fname, s, m))
						}
					}
					return true
				})
			}
		}
		for _, df := range defs {
			fmt.Printf("DEF %s.%s | %q | %s\n", df.pkg, df.name, df.msg, df.loc)
			for _, u := range dedup(uses[df.name]) {
				fmt.Printf("    USE %s\n", u)
			}
		}
		fmt.Println("INLINE:")
		for _, s := range inline {
			fmt.Println("  " + s)
		}
	}
}

func splitLoc(s string) (string, int) {
	i := strings.LastIndex(s, ":")
	n, _ := strconv.Atoi(s[i+1:])
	return s[:i], n
}

func dedup(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func argsFields(ce *ast.CallExpr) []string {
	if ce.Ellipsis.IsValid() {
		return []string{src(ce.Args[len(ce.Args)-1]) + "..."}
	}
	return pairs(ce.Args[1:])
}
