package analyzers

import (
	"fmt"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/ianflanagan1/go-ianf-strict-baseline/internal/core/baseline"
)

// CoreReachesNothingImpure returns the analyzer that keeps impure operations
// out of the code the core calls and loads. It judges each function by what
// its code does, never by its package's name or where it lives: it runs on
// every package the project depends on, the standard library excepted, and
// exports a fact for each function that reaches an impure operation, with
// the path to it, and for each package whose load-time code does. In the
// project's core it reports each call that reaches one outside the core,
// and each import whose loading does.
//
// The standard library is a leaf, judged only by impureOperations. A call
// on a value the function was passed is its caller's, and is judged where
// the caller passes it. A call on a value the function built is resolved
// through SSA to the types it can build; on a value it can't trace, such as
// one another function returned, it reaches every implementation that the
// package can see, and a function value it can't trace can't be shown pure.
func CoreReachesNothingImpure(p Project) *analysis.Analyzer {
	ops := impureOperations()
	return &analysis.Analyzer{
		Name:      "corereach",
		Doc:       "reports calls and imports in the core that reach an impure operation",
		FactTypes: []analysis.Fact{new(reachFact), new(loadFact)},
		Run: func(pass *analysis.Pass) (any, error) {
			if isStandard(pass.Pkg.Path()) {
				return nil, nil
			}
			r := newReachPass(ops, pass, p.isOwnCore)
			r.settle()
			r.exportFacts()
			if p.isOwnCore(pass.Pkg) {
				reportReach(r)
			}
			return nil, nil
		},
	}
}

// reachFact marks a function that reaches an impure operation, or that
// calls through a value it is passed. The analysis framework encodes facts
// with gob, which reads only exported fields.
type reachFact struct {
	// Path runs from the function to the impure operation, which is last,
	// and is empty when the function reaches none itself.
	Path      []string
	Operation string
	// Dynamic lists the parameters, the receiver first, whose methods or
	// function values the function calls, which its caller decides.
	Dynamic []int
}

func (*reachFact) AFact() {}

// loadFact marks a package whose load-time code, or that of a package it
// imports at any depth, reaches an impure operation.
type loadFact struct {
	// Path runs from the package to the impure operation, which is last.
	Path      []string
	Operation string
}

func (*loadFact) AFact() {}

// isOwnCore reports whether pkg is in the project's own core, whose code
// reports what it reaches.
func (p Project) isOwnCore(pkg *types.Package) bool {
	if pkg == nil {
		return false
	}
	role, own := p.role(pkg.Path())
	return own && role == baseline.Core
}

// reportReach reports each call in a core package that reaches an impure
// operation outside the core, and each import whose loading does.
func reportReach(r *reachPass) {
	for _, fn := range r.funcs {
		if !isTestFile(r.pass, fn.Pos()) {
			reportCalls(r, r.verdicts[fn])
		}
	}
	for _, file := range r.pass.Files {
		if isTestFile(r.pass, file.Pos()) {
			continue
		}
		for _, spec := range file.Imports {
			reportImport(r, spec.Pos(), spec.Path.Value)
		}
	}
}

func reportCalls(r *reachPass, v *verdict) {
	for _, f := range v.findings {
		if f.outside && f.pos.IsValid() {
			r.pass.Reportf(f.pos, "%s", callMessage(f))
		}
	}
}

func reportImport(r *reachPass, pos token.Pos, quoted string) {
	path, err := strconv.Unquote(quoted)
	if err != nil {
		return
	}
	for _, imported := range r.pass.Pkg.Imports() {
		var fact loadFact
		if imported.Path() == path && !r.isOwnCore(imported) && r.pass.ImportPackageFact(imported, &fact) {
			r.pass.Reportf(pos, "importing %s runs load-time code that reaches %s, which %s: %s; "+
				"import only packages whose loading does nothing impure, or move the import to the shell",
				path, fact.Path[len(fact.Path)-1], fact.Operation, strings.Join(fact.Path, " → "))
		}
	}
}

func callMessage(f finding) string {
	const fix = "take the value as an argument instead, or move the call to the shell"
	last := f.path[len(f.path)-1]
	if len(f.path) == 1 {
		return fmt.Sprintf("this call reaches %s, which %s; %s", last, f.operation, fix)
	}
	return fmt.Sprintf("calling %s reaches %s, which %s: %s; %s", f.path[0], last, f.operation, strings.Join(f.path, " → "), fix)
}
