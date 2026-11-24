package analyzers

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"
)

// unresolved is what a call through a value the analysis can't trace does.
const unresolved = "can't be resolved, so can't be shown pure"

// reachPass works out, for one package, which of its functions reach an
// impure operation.
type reachPass struct {
	ops       operations
	pass      *analysis.Pass
	isOwnCore func(*types.Package) bool
	pkg       *ssa.Package
	// funcs are the package's functions in source order, its initializer
	// first, each followed by the functions declared inside it.
	funcs    []*ssa.Function
	verdicts map[*ssa.Function]*verdict
	// visible are the named types the package can see: its own and those
	// of every package it imports at any depth.
	visible []types.Type
	// implementations holds, by interface and method, what the method's
	// implementations reach; pending those being worked out.
	implementations map[string]summary
	pending         map[string]bool
}

// slot is a parameter of a function, the receiver first, or a free
// variable of a closure.
type slot struct {
	free  bool
	index int
}

// finding is one way a function reaches an impure operation.
type finding struct {
	// path runs from what the function calls to the operation, which is
	// last.
	path      []string
	operation string
	pos       token.Pos
	// outside is true when the first step leaves the project's core, or
	// can't be resolved.
	outside bool
}

type verdict struct {
	findings []finding
	dynamic  map[slot]bool
}

// summary is what calling a function does, as its callers see it.
type summary struct {
	name string
	// pkg declares the function, and is nil when it can't be resolved.
	pkg *types.Package
	// path runs from the function to the impure operation it reaches, and
	// is nil when it reaches none.
	path      []string
	operation string
	dynamic   []slot
}

func newReachPass(ops operations, pass *analysis.Pass, isOwnCore func(*types.Package) bool) *reachPass {
	prog := ssa.NewProgram(pass.Fset, ssa.BuilderMode(0))
	for _, imported := range pass.Pkg.Imports() {
		prog.CreatePackage(imported, nil, nil, true)
	}
	pkg := prog.CreatePackage(pass.Pkg, pass.Files, pass.TypesInfo, false)
	pkg.Build()
	return &reachPass{
		ops: ops, pass: pass, isOwnCore: isOwnCore, pkg: pkg,
		funcs:    sourceFunctions(pass, pkg),
		verdicts: map[*ssa.Function]*verdict{},
		pending:  map[string]bool{},
	}
}

func sourceFunctions(pass *analysis.Pass, pkg *ssa.Package) []*ssa.Function {
	funcs := withAnonymous(pkg.Func("init"))
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			if fn := declaredFunction(pass, pkg, decl); fn != nil {
				funcs = append(funcs, withAnonymous(fn)...)
			}
		}
	}
	return funcs
}

func declaredFunction(pass *analysis.Pass, pkg *ssa.Package, decl ast.Decl) *ssa.Function {
	funcDecl, ok := decl.(*ast.FuncDecl)
	if !ok {
		return nil
	}
	obj, ok := pass.TypesInfo.Defs[funcDecl.Name].(*types.Func)
	if !ok {
		return nil
	}
	return pkg.Prog.FuncValue(obj)
}

// withAnonymous returns fn and the functions declared inside it, at any
// depth.
func withAnonymous(fn *ssa.Function) []*ssa.Function {
	funcs := []*ssa.Function{fn}
	for _, anon := range fn.AnonFuncs {
		funcs = append(funcs, withAnonymous(anon)...)
	}
	return funcs
}

// settle works out every function's verdict, again until none changes, so
// that calls within the package, recursive ones included, see their
// callees' final verdicts.
func (r *reachPass) settle() {
	for changed := true; changed; {
		changed = false
		r.implementations = map[string]summary{}
		for _, fn := range r.funcs {
			if r.evaluate(fn) {
				changed = true
			}
		}
	}
}

// evaluate works out fn's verdict, and reports whether it changed.
func (r *reachPass) evaluate(fn *ssa.Function) bool {
	e := &evaluation{reach: r, fn: fn, initializer: isInitializer(fn), result: &verdict{dynamic: map[slot]bool{}}, passing: map[ssa.Value]bool{}}
	for _, block := range liveBlocks(fn) {
		for _, instr := range block.Instrs {
			e.instruction(instr)
		}
	}
	old := r.verdicts[fn]
	r.verdicts[fn] = e.result
	return old == nil || len(old.findings) != len(e.result.findings) || len(old.dynamic) != len(e.result.dynamic)
}

// liveBlocks returns fn's blocks that can run: those reachable from its
// entry, or its recover block, without taking the branch a constant
// condition rules out, such as code behind `if debug` where debug is a
// false constant.
func liveBlocks(fn *ssa.Function) []*ssa.BasicBlock {
	var live []*ssa.BasicBlock
	seen := map[*ssa.BasicBlock]bool{}
	queue := slices.Clone(fn.Blocks[:min(1, len(fn.Blocks))])
	if fn.Recover != nil {
		queue = append(queue, fn.Recover)
	}
	for len(queue) > 0 {
		block := queue[0]
		queue = queue[1:]
		if seen[block] {
			continue
		}
		seen[block] = true
		live = append(live, block)
		queue = append(queue, successors(block)...)
	}
	return live
}

// successors returns the blocks that can follow block: one of an if's two
// when its condition is a constant.
func successors(block *ssa.BasicBlock) []*ssa.BasicBlock {
	if len(block.Instrs) == 0 {
		return block.Succs
	}
	branch, ok := block.Instrs[len(block.Instrs)-1].(*ssa.If)
	if !ok {
		return block.Succs
	}
	condition, ok := branch.Cond.(*ssa.Const)
	if !ok || condition.Value == nil {
		return block.Succs
	}
	if constant.BoolVal(condition.Value) {
		return block.Succs[:1]
	}
	return block.Succs[1:]
}

// isInitializer reports whether fn is load-time code: the package's
// initializer, an init function, or a function declared inside one. Only
// they may write package-level variables.
func isInitializer(fn *ssa.Function) bool {
	top := fn
	for top.Parent() != nil {
		top = top.Parent()
	}
	if top.Synthetic == "package initializer" {
		return true
	}
	obj, ok := top.Object().(*types.Func)
	return ok && obj.Name() == "init" && obj.Signature().Recv() == nil
}

// exportFacts exports a fact for each of the package's functions that
// reaches an impure operation or calls through a value it is passed, and
// one for the package when its loading reaches one.
func (r *reachPass) exportFacts() {
	for _, fn := range r.funcs {
		r.exportFact(fn)
	}
	if path, operation := r.load(); path != nil {
		r.pass.ExportPackageFact(&loadFact{Path: path, Operation: operation})
	}
}

func (r *reachPass) exportFact(fn *ssa.Function) {
	obj, ok := fn.Object().(*types.Func)
	if !ok || obj.Pkg() != r.pass.Pkg || isInitializer(fn) {
		return
	}
	s := r.local(fn, obj.FullName())
	if s.path == nil && len(s.dynamic) == 0 {
		return
	}
	fact := &reachFact{Path: s.path, Operation: s.operation}
	for _, sl := range s.dynamic {
		if !sl.free {
			fact.Dynamic = append(fact.Dynamic, sl.index)
		}
	}
	r.pass.ExportObjectFact(obj, fact)
}

// load returns the path from the package to an impure operation its
// loading reaches, through its own initializer or an import's.
func (r *reachPass) load() ([]string, string) {
	self := r.pass.Pkg.Path()
	if f := r.verdicts[r.pkg.Func("init")].first(); f != nil {
		return append([]string{self}, f.path...), f.operation
	}
	imports := slices.SortedFunc(slices.Values(r.pass.Pkg.Imports()), func(a, b *types.Package) int {
		return strings.Compare(a.Path(), b.Path())
	})
	for _, imported := range imports {
		var fact loadFact
		if r.pass.ImportPackageFact(imported, &fact) {
			return append([]string{self}, fact.Path...), fact.Operation
		}
	}
	return nil, ""
}

func (v *verdict) first() *finding {
	if v == nil || len(v.findings) == 0 {
		return nil
	}
	return &v.findings[0]
}

// summarise returns what calling fn does.
func (r *reachPass) summarise(fn *ssa.Function) summary {
	origin := fn
	if fn.Origin() != nil {
		origin = fn.Origin()
	}
	obj, ok := origin.Object().(*types.Func)
	if !ok {
		if origin.Pkg == r.pkg {
			return r.local(origin, origin.String())
		}
		// Another package's initializer, whose loading is that package's
		// fact.
		return summary{name: origin.String(), pkg: functionPackage(origin)}
	}
	s := r.object(obj)
	if strings.HasPrefix(fn.Synthetic, "bound method wrapper") {
		s.dynamic = boundSlots(s.dynamic)
	}
	return s
}

// boundSlots turns a method's slots into those of its bound method value,
// whose receiver is its one free variable.
func boundSlots(method []slot) []slot {
	bound := make([]slot, 0, len(method))
	for _, sl := range method {
		if sl.index == 0 {
			bound = append(bound, slot{free: true})
		} else {
			bound = append(bound, slot{index: sl.index - 1})
		}
	}
	return bound
}

// object returns what calling the function or method obj does.
func (r *reachPass) object(obj *types.Func) summary {
	origin := obj.Origin()
	if recv := origin.Signature().Recv(); recv != nil && types.IsInterface(recv.Type()) {
		return r.everyImplementation(recv.Type(), origin)
	}
	switch {
	case isStandard(origin.Pkg().Path()):
		return r.standard(origin)
	case origin.Pkg() != r.pass.Pkg:
		return r.fromFact(origin)
	default:
		return r.local(r.pkg.Prog.FuncValue(origin), origin.FullName())
	}
}

// standard judges a standard-library function by the map alone. It may
// call through any of its parameters that can hold code.
func (r *reachPass) standard(obj *types.Func) summary {
	s := summary{name: obj.FullName(), pkg: obj.Pkg()}
	if operation, ok := r.ops.ofFunc(obj); ok {
		s.path, s.operation = []string{s.name}, operation
	}
	sig := obj.Signature()
	offset := 0
	if sig.Recv() != nil {
		offset = 1
	}
	for i := range sig.Params().Len() {
		if hidesCode(sig.Params().At(i).Type()) {
			s.dynamic = append(s.dynamic, slot{index: i + offset})
		}
	}
	return s
}

func (r *reachPass) fromFact(obj *types.Func) summary {
	s := summary{name: obj.FullName(), pkg: obj.Pkg()}
	var fact reachFact
	if !r.pass.ImportObjectFact(obj, &fact) {
		return s
	}
	for _, index := range fact.Dynamic {
		s.dynamic = append(s.dynamic, slot{index: index})
	}
	if len(fact.Path) > 0 {
		s.path, s.operation = fact.Path, fact.Operation
	}
	return s
}

// local returns what calling fn, one of the package's own functions, does,
// as far as the verdicts so far go.
func (r *reachPass) local(fn *ssa.Function, name string) summary {
	s := summary{name: name, pkg: r.pass.Pkg}
	v := r.verdicts[fn]
	if v == nil {
		return s
	}
	s.dynamic = slices.SortedFunc(maps.Keys(v.dynamic), func(a, b slot) int {
		if a.free != b.free {
			if a.free {
				return 1
			}
			return -1
		}
		return a.index - b.index
	})
	if f := v.first(); f != nil {
		s.path, s.operation = append([]string{name}, f.path...), f.operation
	}
	return s
}

// everyImplementation returns what calling method m on a value of
// interface type iface does, when the value can't be traced: the first
// implementation that the package can see that reaches an impure operation.
func (r *reachPass) everyImplementation(iface types.Type, m *types.Func) summary {
	key := types.TypeString(iface, nil) + "." + m.Name()
	if s, done := r.implementations[key]; done {
		return s
	}
	if r.pending[key] {
		return summary{name: key}
	}
	r.pending[key] = true
	s := r.searchImplementations(iface, m)
	delete(r.pending, key)
	r.implementations[key] = s
	return s
}

func (r *reachPass) searchImplementations(iface types.Type, m *types.Func) summary {
	methods, ok := iface.Underlying().(*types.Interface)
	if !ok {
		return summary{name: m.FullName()}
	}
	for _, t := range r.visibleTypes() {
		pointer := types.NewPointer(t)
		if !types.Implements(pointer, methods) {
			continue
		}
		if impl := lookupMethod(pointer, m); impl != nil {
			if s := r.object(impl); s.path != nil {
				return s
			}
		}
	}
	return summary{name: m.FullName()}
}

func (r *reachPass) visibleTypes() []types.Type {
	if r.visible != nil {
		return r.visible
	}
	r.visible = []types.Type{}
	seen := map[*types.Package]bool{}
	queue := []*types.Package{r.pass.Pkg}
	for len(queue) > 0 {
		pkg := queue[0]
		queue = queue[1:]
		if seen[pkg] {
			continue
		}
		seen[pkg] = true
		r.visible = append(r.visible, namedTypes(pkg)...)
		queue = append(queue, pkg.Imports()...)
	}
	return r.visible
}

// namedTypes returns pkg's package-level named types that aren't interfaces
// or generic.
func namedTypes(pkg *types.Package) []types.Type {
	var list []types.Type
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		typeName, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || typeName.IsAlias() {
			continue
		}
		if named, ok := typeName.Type().(*types.Named); ok && named.TypeParams().Len() == 0 && !types.IsInterface(named) {
			list = append(list, named)
		}
	}
	return list
}

// lookupMethod returns t's method with m's name, or nil.
func lookupMethod(t types.Type, m *types.Func) *types.Func {
	obj, _, _ := types.LookupFieldOrMethod(t, true, m.Pkg(), m.Name())
	method, ok := obj.(*types.Func)
	if !ok {
		return nil
	}
	return method
}

// methodsCalled returns the methods of t, a concrete type passed as a value
// of type param, that the callee may call: the interface's methods, or
// every method when the interface has none, since the callee may assert it
// to any interface.
func methodsCalled(param, t types.Type) []*types.Func {
	iface, ok := param.Underlying().(*types.Interface)
	if !ok || iface.NumMethods() == 0 {
		return everyMethod(t)
	}
	var methods []*types.Func
	for m := range iface.Methods() {
		if impl := lookupMethod(t, m); impl != nil {
			methods = append(methods, impl)
		}
	}
	return methods
}

// everyMethod returns the methods of t or, when t isn't a pointer, of a
// pointer to it.
func everyMethod(t types.Type) []*types.Func {
	receiver := t
	if _, isPointer := t.Underlying().(*types.Pointer); !isPointer && !types.IsInterface(t) {
		receiver = types.NewPointer(t)
	}
	var methods []*types.Func
	for sel := range types.NewMethodSet(receiver).Methods() {
		if method, ok := sel.Obj().(*types.Func); ok {
			methods = append(methods, method)
		}
	}
	return methods
}

// hidesCode reports whether a value of type t is code that runs where it's
// passed: an interface, a function, or a pointer, slice, array, channel or
// map of them. A function that calls through a field of a struct it is passed is
// taken to call what its caller stored there, and isn't followed further.
func hidesCode(t types.Type) bool {
	return len(codeHolders(t)) > 0
}

// codeHolders returns the interface and function types of the code a value
// of type t is: t itself, or its elements.
func codeHolders(t types.Type) []types.Type {
	var holders []types.Type
	collectHolders(t, map[types.Type]bool{}, &holders)
	return holders
}

func collectHolders(t types.Type, seen map[types.Type]bool, holders *[]types.Type) {
	if seen[t] {
		return
	}
	seen[t] = true
	switch u := t.Underlying().(type) {
	case *types.Interface, *types.Signature:
		*holders = append(*holders, t)
	case *types.Pointer:
		collectHolders(u.Elem(), seen, holders)
	case *types.Slice:
		collectHolders(u.Elem(), seen, holders)
	case *types.Array:
		collectHolders(u.Elem(), seen, holders)
	case *types.Chan:
		collectHolders(u.Elem(), seen, holders)
	case *types.Map:
		collectHolders(u.Key(), seen, holders)
		collectHolders(u.Elem(), seen, holders)
	}
}
