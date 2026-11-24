package analyzers

import (
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// evaluation works out one function's verdict from its instructions.
type evaluation struct {
	reach       *reachPass
	fn          *ssa.Function
	initializer bool
	result      *verdict
	// passing holds the values being followed into a callee, so that a
	// value holding itself is followed once.
	passing map[ssa.Value]bool
}

func (e *evaluation) found(f finding) {
	e.result.findings = append(e.result.findings, f)
}

// direct records an impure operation the function performs itself.
func (e *evaluation) direct(name, operation string, pos token.Pos) {
	e.found(finding{path: []string{name}, operation: operation, pos: pos})
}

// reached records a call, at pos, of a function that s summarises.
func (e *evaluation) reached(prefix []string, s summary, pos token.Pos) {
	if s.path == nil {
		return
	}
	e.found(finding{
		path: slices.Concat(prefix, s.path), operation: s.operation, pos: pos,
		outside: s.pkg == nil || !e.reach.isOwnCore(s.pkg),
	})
}

func (e *evaluation) unresolved(prefix []string, pos token.Pos) {
	e.found(finding{path: append(slices.Clone(prefix), "a function value"), operation: unresolved, pos: pos, outside: true})
}

func (e *evaluation) instruction(instr ssa.Instruction) {
	switch i := instr.(type) {
	case ssa.CallInstruction:
		e.call(i)
	case *ssa.Store:
		e.write(i.Addr, i.Pos())
	case *ssa.MapUpdate:
		e.write(i.Map, i.Pos())
	case *ssa.MakeClosure:
		e.closure(i)
	case *ssa.Convert:
		if isUnsafePointer(i.X.Type()) || isUnsafePointer(i.Type()) {
			e.direct("unsafe.Pointer", "is a conversion the type system can't check", i.Pos())
		}
	}
	for _, operand := range instr.Operands(nil) {
		if global, ok := (*operand).(*ssa.Global); ok {
			e.standardGlobal(global, instr.Pos())
		}
	}
}

func isUnsafePointer(t types.Type) bool {
	return types.Identical(t.Underlying(), types.Typ[types.UnsafePointer])
}

// write records a write to a package-level variable, which only load-time
// code may make.
func (e *evaluation) write(addr ssa.Value, pos token.Pos) {
	if global := rootGlobal(addr); global != nil && !e.initializer {
		e.direct(global.String(), "is a package-level variable written outside load-time code", pos)
	}
}

// rootGlobal returns the package-level variable that addr points into,
// directly or through a pointer, slice or map it holds, or nil.
func rootGlobal(addr ssa.Value) *ssa.Global {
	switch a := addr.(type) {
	case *ssa.Global:
		return a
	case *ssa.FieldAddr:
		return rootGlobal(a.X)
	case *ssa.IndexAddr:
		return rootGlobal(a.X)
	case *ssa.UnOp:
		if a.Op == token.MUL {
			return rootGlobal(a.X)
		}
	}
	return nil
}

// globalAddress returns the package-level variable whose address, or the
// address of a part of it, v is, or nil.
func globalAddress(v ssa.Value) *ssa.Global {
	switch a := v.(type) {
	case *ssa.Global:
		return a
	case *ssa.FieldAddr:
		return globalAddress(a.X)
	case *ssa.IndexAddr:
		return globalAddress(a.X)
	}
	return nil
}

func (e *evaluation) standardGlobal(global *ssa.Global, pos token.Pos) {
	obj := global.Object()
	if obj == nil || obj.Pkg() == nil || !isStandard(obj.Pkg().Path()) {
		return
	}
	if operation, ok := e.reach.ops.ofVar(global.String(), obj.Pkg().Path(), obj.Type()); ok {
		e.direct(global.String(), operation, pos)
	}
}

// closure follows the values a closure captures into the calls it makes
// through them.
func (e *evaluation) closure(mc *ssa.MakeClosure) {
	fn, ok := mc.Fn.(*ssa.Function)
	if !ok {
		return
	}
	s := e.reach.summarise(fn)
	for _, sl := range s.dynamic {
		if sl.free && sl.index < len(mc.Bindings) {
			e.argument(s.name, mc.Bindings[sl.index], mc.Pos())
		}
	}
}

func (e *evaluation) call(c ssa.CallInstruction) {
	common := c.Common()
	pos := c.Pos()
	if builtin, ok := common.Value.(*ssa.Builtin); ok {
		e.builtin(builtin, pos)
		return
	}
	if !e.initializer {
		for _, arg := range slices.Concat([]ssa.Value{common.Value}, common.Args) {
			if global := globalAddress(arg); global != nil {
				e.direct(global.String(), "is a package-level variable whose address is passed to a call that may write it", pos)
			}
		}
	}
	switch {
	case common.IsInvoke():
		e.invoke(common, pos)
	case common.StaticCallee() != nil:
		e.static(common.StaticCallee(), common, pos)
	default:
		e.functionValue(common.Value, pos)
	}
}

func (e *evaluation) builtin(builtin *ssa.Builtin, pos token.Pos) {
	switch builtin.Name() {
	case "print", "println":
		e.direct(builtin.Name(), "writes to standard error", pos)
	case "Add", "Slice", "SliceData", "String", "StringData":
		e.direct("unsafe."+builtin.Name(), "is an operation the type system can't check", pos)
	}
}

func (e *evaluation) static(callee *ssa.Function, common *ssa.CallCommon, pos token.Pos) {
	if strings.HasPrefix(callee.Name(), "_Cfunc_") {
		e.direct(callee.Name(), "calls C code through cgo", pos)
		return
	}
	s := e.reach.summarise(callee)
	if s.path != nil {
		// The core's own standard-library calls are core-does-nothing-impure's.
		outside := !isStandard(s.pkg.Path()) && !e.reach.isOwnCore(s.pkg)
		e.found(finding{path: s.path, operation: s.operation, pos: pos, outside: outside})
	}
	e.arguments(s, common.Args, 0, pos)
	e.typeArguments(callee, s.name, pos)
}

// arguments follows the arguments the callee s calls through. shift is
// how many of the callee's parameters args leaves out at the start.
func (e *evaluation) arguments(s summary, args []ssa.Value, shift int, pos token.Pos) {
	for _, sl := range s.dynamic {
		if i := sl.index - shift; !sl.free && i >= 0 && i < len(args) {
			e.argument(s.name, args[i], pos)
		}
	}
}

// argument follows a value passed to callee, which may call through it.
func (e *evaluation) argument(callee string, arg ssa.Value, pos token.Pos) {
	if !hidesCode(arg.Type()) || e.passing[arg] {
		return
	}
	e.passing[arg] = true
	defer delete(e.passing, arg)
	o := e.sources(arg)
	e.callersValues(o)
	prefix := []string{callee}
	for _, fn := range o.funcs {
		e.reached(prefix, e.reach.summarise(fn), pos)
	}
	for _, box := range o.boxes {
		e.passedBox(prefix, arg.Type(), box, pos)
	}
	if o.unknown {
		e.untraced(prefix, arg.Type(), pos)
	}
}

// passedBox follows the methods a callee may call on a concrete value
// passed to it as an interface value of type param.
func (e *evaluation) passedBox(prefix []string, param types.Type, box *ssa.MakeInterface, pos token.Pos) {
	for _, m := range methodsCalled(param, box.X.Type()) {
		s := e.reach.object(m)
		e.reached(prefix, s, pos)
		if slices.Contains(s.dynamic, slot{index: 0}) {
			e.argument(s.name, box.X, pos)
		}
	}
}

// untraced follows a value of type t that the analysis can't trace to where
// it was built: each method of an interface it holds reaches every
// implementation, and a function value it holds can't be resolved. What a
// callee does with a value of an empty interface isn't followed.
func (e *evaluation) untraced(prefix []string, t types.Type, pos token.Pos) {
	for _, holder := range codeHolders(t) {
		iface, ok := holder.Underlying().(*types.Interface)
		if !ok {
			e.unresolved(prefix, pos)
			continue
		}
		for m := range iface.Methods() {
			e.reached(prefix, e.reach.everyImplementation(holder, m), pos)
		}
	}
}

// callersValues records the parameters and free variables a value comes
// from, which the function's callers decide.
func (e *evaluation) callersValues(o sources) {
	for _, index := range o.params {
		e.result.dynamic[slot{index: index}] = true
	}
	for _, index := range o.free {
		e.result.dynamic[slot{free: true, index: index}] = true
	}
}

// typeArguments follows the methods that a generic callee may call on its
// type arguments.
func (e *evaluation) typeArguments(callee *ssa.Function, name string, pos token.Pos) {
	origin := callee.Origin()
	if origin == nil {
		return
	}
	params := origin.TypeParams()
	for i, arg := range callee.TypeArgs() {
		if _, isParam := arg.(*types.TypeParam); isParam || i >= params.Len() {
			continue
		}
		if constraint, ok := params.At(i).Constraint().Underlying().(*types.Interface); ok {
			e.constraintMethods(name, arg, constraint, pos)
		}
	}
}

func (e *evaluation) constraintMethods(callee string, arg types.Type, constraint *types.Interface, pos token.Pos) {
	for m := range constraint.Methods() {
		if impl := lookupMethod(arg, m); impl != nil {
			e.reached([]string{callee}, e.reach.object(impl), pos)
		}
	}
}

// invoke follows a call of a method on an interface value.
func (e *evaluation) invoke(common *ssa.CallCommon, pos token.Pos) {
	m := common.Method
	if operation, ok := e.reach.ops.named[m.FullName()]; ok {
		e.direct(m.FullName(), operation, pos)
		return
	}
	o := e.sources(common.Value)
	e.callersValues(o)
	for _, box := range o.boxes {
		impl := lookupMethod(box.X.Type(), m)
		if impl == nil {
			continue
		}
		s := e.reach.object(impl)
		e.reached(nil, s, pos)
		e.arguments(s, common.Args, 1, pos)
		if slices.Contains(s.dynamic, slot{index: 0}) {
			e.argument(s.name, box.X, pos)
		}
	}
	if o.unknown {
		e.reached(nil, e.reach.everyImplementation(common.Value.Type(), m), pos)
	}
}

// functionValue follows a call of a function value.
func (e *evaluation) functionValue(value ssa.Value, pos token.Pos) {
	o := e.sources(value)
	e.callersValues(o)
	for _, fn := range o.funcs {
		e.reached(nil, e.reach.summarise(fn), pos)
	}
	if o.unknown {
		e.unresolved(nil, pos)
	}
}
