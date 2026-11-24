// Package lowlevel uses builtins, unsafe and the standard library's
// package-level variables.
package lowlevel

import (
	"time"
	"unsafe"
)

func Trace(s string) string {
	println(s)
	return s
}

func Bytes(s string) []byte { return unsafe.Slice(unsafe.StringData(s), len(s)) }

func Zone() string { return time.Local.String() }
