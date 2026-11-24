// Package purelib is third-party code that does nothing impure.
package purelib

import "strings"

func Shout(s string) string { return strings.ToUpper(s) + "!" }
