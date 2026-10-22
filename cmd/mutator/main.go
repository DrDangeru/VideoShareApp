// Command mutator performs local AST-based mutation testing for the Go
// application package. It generates one small bug per mutant (swapped
// comparisons, flipped boolean operators, removed negations, bumped integer
// literals), applies it to a throwaway copy of the working tree, and expects
// the Go test suite to fail. Surviving mutants name potential coverage gaps.
//
// This tool is run on demand by the user (typically before a deploy) because
// a full run executes the test suite hundreds of times; see TESTING.md.
//
// Usage:
//
//	go run ./cmd/mutator                 # full run
//	go run ./cmd/mutator -dry-run        # list mutation sites only
//	go run ./cmd/mutator -only handlers  # restrict to matching files
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/tools/go/ast/astutil"
)

var binopSwap = map[token.Token]token.Token{
	token.EQL: token.NEQ, token.NEQ: token.EQL,
	token.LSS: token.LEQ, token.LEQ: token.LSS,
	token.GTR: token.GEQ, token.GEQ: token.GTR,
	token.LAND: token.LOR, token.LOR: token.LAND,
	token.ADD: token.SUB, token.SUB: token.ADD,
}

type mutant struct {
	file string
	site int
	pos  string
	desc string
}

type result struct {
	mutant mutant
	status string // killed | survived | invalid
}

func main() {
	only := flag.String("only", "", "restrict to files whose path contains this substring")
	dir := flag.String("dir", "internal/app", "package directory to mutate")
	dryRun := flag.Bool("dry-run", false, "list mutation sites without running tests")
	workers := flag.Int("workers", 4, "parallel worker copies")
	timeout := flag.Duration("timeout", 3*time.Minute, "per-mutant build+test timeout")
	flag.Parse()

	root, err := os.Getwd()
	if err != nil {
		fatal(err)
	}

	files, err := productionGoFiles(filepath.Join(root, *dir))
	if err != nil {
		fatal(err)
	}
	files = filterFiles(files, *only)

	// Enumerate mutation sites with one traversal order shared by enumeration
	// and application, so site indexes stay consistent.
	var mutants []mutant
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			fatal(err)
		}
		sites, err := enumerateSites(src, file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skipping %s: %v\n", file, err)
			continue
		}
		for i, site := range sites {
			mutants = append(mutants, mutant{file: file, site: i, pos: site.pos, desc: site.desc})
		}
	}

	fmt.Printf("%d mutation sites in %d files\n", len(mutants), len(files))
	if *dryRun {
		for _, m := range mutants {
			fmt.Printf("  %s: %s\n", m.pos, m.desc)
		}
		return
	}
	if len(mutants) == 0 {
		return
	}

	if *workers < 1 {
		*workers = 1
	}
	base := mktemp()
	defer os.RemoveAll(base)
	copyTree(root, base, nil)

	workerDirs := make([]string, *workers)
	for i := range workerDirs {
		workerDirs[i] = mktemp()
		defer os.RemoveAll(workerDirs[i])
		copyTree(base, workerDirs[i], nil)
	}

	pristine := make(map[string][]byte)
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			fatal(err)
		}
		pristine[file] = src
	}

	results := make(chan result)
	var wg sync.WaitGroup
	next := make(chan mutant, len(mutants))
	for _, m := range mutants {
		next <- m
	}
	close(next)

	for _, dir := range workerDirs {
		wg.Add(1)
		go func(workerDir string) {
			defer wg.Done()
			for m := range next {
				results <- result{m, runMutant(workerDir, m, pristine, *timeout)}
			}
		}(dir)
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	killed, survived, invalid := 0, 0, 0
	var survivors []result
	for r := range results {
		fmt.Printf("  %-8s %s: %s\n", r.status, r.mutant.pos, r.mutant.desc)
		switch r.status {
		case "killed":
			killed++
		case "survived":
			survived++
			survivors = append(survivors, r)
		default:
			invalid++
		}
	}

	fmt.Printf("\n%d killed, %d survived, %d invalid, of %d mutants\n",
		killed, survived, invalid, len(mutants))
	for _, s := range survivors {
		fmt.Printf("SURVIVOR %s: %s\n", s.mutant.pos, s.mutant.desc)
	}
	if survived > 0 || invalid > 0 {
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "mutator:", err)
	os.Exit(1)
}

func mktemp() string {
	dir, err := os.MkdirTemp("", "vsmutator-")
	if err != nil {
		fatal(err)
	}
	return dir
}

// productionGoFiles lists mutable Go files: no tests, no build-tagged files.
func productionGoFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if hasBuildTag(src) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, err
}

func hasBuildTag(src []byte) bool {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.PackageClauseOnly|parser.ParseComments)
	if err != nil {
		return false
	}
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if strings.HasPrefix(comment.Text, "//go:build ") {
				return true
			}
		}
	}
	return false
}

func filterFiles(files []string, only string) []string {
	if only == "" {
		return files
	}
	var kept []string
	for _, file := range files {
		if strings.Contains(filepath.ToSlash(file), only) {
			kept = append(kept, file)
		}
	}
	return kept
}

type site struct {
	pos  string
	desc string
}

// enumerateSites records every mutable node in one astutil.Apply traversal,
// the same traversal used when applying a chosen site.
func enumerateSites(src []byte, path string) ([]site, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var sites []site
	astutil.Apply(file, func(c *astutil.Cursor) bool {
		switch n := c.Node().(type) {
		case *ast.BinaryExpr:
			if swap, ok := binopSwap[n.Op]; ok {
				sites = append(sites, site{
					pos:  position(fset, n.OpPos),
					desc: fmt.Sprintf("operator %s -> %s", n.Op, swap),
				})
			}
		case *ast.UnaryExpr:
			if n.Op == token.NOT {
				sites = append(sites, site{
					pos:  position(fset, n.OpPos),
					desc: "remove '!'",
				})
			}
		case *ast.BasicLit:
			if n.Kind == token.INT {
				sites = append(sites, site{
					pos:  position(fset, n.ValuePos),
					desc: fmt.Sprintf("literal %s -> %s", n.Value, bumpLiteral(n.Value)),
				})
			}
		case *ast.Ident:
			if n.Name == "true" || n.Name == "false" {
				flip := "true"
				if n.Name == "true" {
					flip = "false"
				}
				sites = append(sites, site{
					pos:  position(fset, n.NamePos),
					desc: fmt.Sprintf("%s -> %s", n.Name, flip),
				})
			}
		}
		return true
	}, nil)
	return sites, nil
}

func position(fset *token.FileSet, pos token.Pos) string {
	p := fset.Position(pos)
	return fmt.Sprintf("%s:%d", filepath.Base(p.Filename), p.Line)
}

func bumpLiteral(value string) string {
	var bumped int
	if _, err := fmt.Sscanf(value, "%d", &bumped); err != nil {
		return value
	}
	return fmt.Sprintf("%d", bumped+1)
}

// mutatedSource re-parses the pristine file and replaces exactly the target
// site index using the identical traversal order.
func mutatedSource(src []byte, path string, target int) ([]byte, bool) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, false
	}
	index := 0
	replaced := false
	astutil.Apply(file, func(c *astutil.Cursor) bool {
		if replaced {
			return false
		}
		mutate := index == target
		switch n := c.Node().(type) {
		case *ast.BinaryExpr:
			swap, ok := binopSwap[n.Op]
			if !ok {
				return true
			}
			if mutate {
				c.Replace(&ast.BinaryExpr{X: n.X, OpPos: n.OpPos, Op: swap, Y: n.Y})
				replaced = true
			}
			index++
		case *ast.UnaryExpr:
			if n.Op != token.NOT {
				return true
			}
			if mutate {
				c.Replace(n.X)
				replaced = true
			}
			index++
		case *ast.BasicLit:
			if n.Kind != token.INT {
				return true
			}
			if mutate {
				c.Replace(&ast.BasicLit{ValuePos: n.ValuePos, Kind: n.Kind, Value: bumpLiteral(n.Value)})
				replaced = true
			}
			index++
		case *ast.Ident:
			if n.Name != "true" && n.Name != "false" {
				return true
			}
			if mutate {
				flip := "true"
				if n.Name == "true" {
					flip = "false"
				}
				c.Replace(&ast.Ident{NamePos: n.NamePos, Name: flip})
				replaced = true
			}
			index++
		}
		return true
	}, nil)
	if !replaced {
		return nil, false
	}

	var out bytes.Buffer
	if err := printer.Fprint(&out, fset, file); err != nil {
		return nil, false
	}
	return out.Bytes(), true
}

func runMutant(workerDir string, m mutant, pristine map[string][]byte, timeout time.Duration) string {
	src, ok := mutatedSource(pristine[m.file], m.file, m.site)
	if !ok {
		return "invalid"
	}
	target := filepath.Join(workerDir, relPath(m.file))
	if err := os.WriteFile(target, src, 0o644); err != nil {
		return "invalid"
	}
	defer func() {
		_ = os.WriteFile(target, pristine[m.file], 0o644)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "./...")
	build.Dir = workerDir
	build.Stdout = io.Discard
	build.Stderr = io.Discard
	if err := build.Run(); err != nil {
		return "invalid"
	}
	test := exec.CommandContext(ctx, "go", "test", "-count=1", "./internal/app")
	test.Dir = workerDir
	test.Stdout = io.Discard
	test.Stderr = io.Discard
	if err := test.Run(); err != nil {
		if ctx.Err() != nil {
			return "invalid"
		}
		return "killed"
	}
	return "survived"
}

// relPath converts an absolute or repo-relative path to a slash path relative
// to the worker copy root.
func relPath(path string) string {
	path = filepath.ToSlash(path)
	if idx := strings.Index(path, "internal/"); idx >= 0 {
		return path[idx:]
	}
	return strings.TrimPrefix(path, "./")
}

// copyTree copies the source tree, skipping heavy, generated, and private
// directories. skip, when non-nil, receives each directory name.
func copyTree(srcRoot, dstRoot string, skip func(string) bool) error {
	return filepath.WalkDir(srcRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			switch name {
			case ".git", "node_modules", "test-results", "playwright-report",
				"data", "Videos", "uploads":
				return filepath.SkipDir
			}
			if skip != nil && skip(name) {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dstRoot, rel), 0o755)
		}
		if strings.HasSuffix(name, ".exe") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		dst := filepath.Join(dstRoot, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, src, 0o644)
	})
}
