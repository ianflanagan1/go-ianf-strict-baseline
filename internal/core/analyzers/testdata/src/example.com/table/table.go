// Package table calls a function value taken from a table.
package table

import "time"

var handlers = map[string]func() int64{
	"zero": func() int64 { return 0 },
	"now":  func() int64 { return time.Now().Unix() },
}

func Run(name string) int64 { return handlers[name]() }

// Local calls a function value from a table it builds itself, which holds
// one that reads the clock.
func Local(name string) int64 {
	local := map[string]func() int64{
		"zero": zero,
		"now":  now,
	}
	return local[name]()
}

// Pure calls a function value from a table it builds itself, whose
// functions are all pure.
func Pure(name string) int64 {
	local := map[string]func() int64{"zero": zero}
	if f, ok := local[name]; ok {
		return f()
	}
	return 0
}

func zero() int64 { return 0 }

func now() int64 { return time.Now().Unix() }
