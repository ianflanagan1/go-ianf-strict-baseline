// Package envchain does nothing impure itself, but loads envinit.
package envchain

import "example.com/envinit"

func Twice(s string) string { return s + s }

func Where() string { return envinit.Home() }
