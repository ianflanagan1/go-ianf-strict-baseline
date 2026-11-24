// Package apply calls a function value its caller passes.
package apply

func Map(xs []int, f func(int) int) []int {
	out := make([]int, 0, len(xs))
	for _, x := range xs {
		out = append(out, f(x))
	}
	return out
}

// Twice returns a closure that calls the function value its caller passes.
func Twice(f func(int) int) func(int) int {
	return func(x int) int { return f(f(x)) }
}

// Measure calls a method on its type argument.
func Measure[T interface{ Area() int }](x T) int { return x.Area() }
