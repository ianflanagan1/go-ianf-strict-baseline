package analyzers

import (
	"go/token"
	"slices"

	"golang.org/x/tools/go/ssa"
)

// sources are where a value can come from, as far as one function's SSA
// shows.
type sources struct {
	// params and free are the function's parameters and free variables it
	// can come from, which its callers decide.
	params []int
	free   []int
	// funcs are the functions it can be.
	funcs []*ssa.Function
	// boxes are where a concrete value was made into an interface value.
	boxes []*ssa.MakeInterface
	// unknown is true when it can come from somewhere the function's SSA
	// doesn't show, such as another function's result or a package-level
	// variable.
	unknown bool
}

func (e *evaluation) sources(v ssa.Value) sources {
	var o sources
	e.trace(v, &o, map[ssa.Value]bool{})
	return o
}

func (e *evaluation) trace(v ssa.Value, o *sources, seen map[ssa.Value]bool) {
	if v == nil || seen[v] {
		return
	}
	seen[v] = true
	switch x := v.(type) {
	case *ssa.Parameter:
		o.params = append(o.params, slices.Index(e.fn.Params, x))
	case *ssa.FreeVar:
		o.free = append(o.free, slices.Index(e.fn.FreeVars, x))
	case *ssa.Function:
		o.funcs = append(o.funcs, x)
	case *ssa.MakeClosure:
		e.trace(x.Fn, o, seen)
	case *ssa.Const:
	case *ssa.MakeInterface:
		o.boxes = append(o.boxes, x)
	case *ssa.Phi:
		for _, edge := range x.Edges {
			e.trace(edge, o, seen)
		}
	case *ssa.Alloc:
		e.traceStores(x, o, seen)
	case *ssa.MakeMap:
		e.traceMapUpdates(x, o, seen)
	case *ssa.Extract:
		e.traceExtract(x, o, seen)
	case *ssa.Call:
		e.traceResult(x.Common(), o, seen)
	default:
		e.traceOperand(v, o, seen)
	}
}

// traceOperand traces a value made from another one that has the same
// sources: a conversion, an assertion, or a part of it.
func (e *evaluation) traceOperand(v ssa.Value, o *sources, seen map[ssa.Value]bool) {
	if operand := sameSource(v); operand != nil {
		e.trace(operand, o, seen)
		return
	}
	o.unknown = true
}

func sameSource(v ssa.Value) ssa.Value {
	switch x := v.(type) {
	case *ssa.ChangeInterface:
		return x.X
	case *ssa.ChangeType:
		return x.X
	case *ssa.Convert:
		return x.X
	case *ssa.MultiConvert:
		return x.X
	case *ssa.SliceToArrayPointer:
		return x.X
	case *ssa.TypeAssert:
		return x.X
	case *ssa.Slice:
		return x.X
	case *ssa.Field:
		return x.X
	case *ssa.FieldAddr:
		return x.X
	case *ssa.Index:
		return x.X
	case *ssa.IndexAddr:
		return x.X
	case *ssa.Lookup:
		return x.X
	case *ssa.UnOp:
		if x.Op == token.MUL {
			return x.X
		}
	}
	return nil
}

// traceResult traces what a call returns. A standard-library function is a
// leaf, so what it returns is standard-library code, or code it was passed:
// a function such as bufio.NewWriter wraps its arguments. What any other
// function returns is unknown.
func (e *evaluation) traceResult(common *ssa.CallCommon, o *sources, seen map[ssa.Value]bool) {
	callee := common.StaticCallee()
	if callee == nil || !isStandard(functionPackage(callee).Path()) {
		o.unknown = true
		return
	}
	for _, arg := range common.Args {
		if hidesCode(arg.Type()) {
			e.trace(arg, o, seen)
		}
	}
}

func (e *evaluation) traceExtract(x *ssa.Extract, o *sources, seen map[ssa.Value]bool) {
	switch tuple := x.Tuple.(type) {
	case *ssa.TypeAssert:
		e.trace(tuple.X, o, seen)
	case *ssa.Lookup:
		e.trace(tuple.X, o, seen)
	case *ssa.Call:
		e.traceResult(tuple.Common(), o, seen)
	default:
		o.unknown = true
	}
}

// traceStores traces what the function stores in a variable or composite
// it allocated, in any of its parts. One it passes to a call may be stored
// in there too, and is unknown.
func (e *evaluation) traceStores(addr ssa.Value, o *sources, seen map[ssa.Value]bool) {
	referrers := addr.Referrers()
	if referrers == nil {
		return
	}
	for _, ref := range *referrers {
		switch r := ref.(type) {
		case *ssa.Store:
			if r.Addr == addr && hidesCode(r.Val.Type()) {
				e.trace(r.Val, o, seen)
			}
		case *ssa.FieldAddr:
			e.traceStores(r, o, seen)
		case *ssa.IndexAddr:
			e.traceStores(r, o, seen)
		case ssa.CallInstruction:
			o.unknown = true
		}
	}
}

func (e *evaluation) traceMapUpdates(m *ssa.MakeMap, o *sources, seen map[ssa.Value]bool) {
	referrers := m.Referrers()
	if referrers == nil {
		return
	}
	for _, ref := range *referrers {
		if update, ok := ref.(*ssa.MapUpdate); ok && update.Map == m {
			e.trace(update.Value, o, seen)
		}
	}
}
