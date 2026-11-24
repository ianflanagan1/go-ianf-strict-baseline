// Package stamp looks pure, but reads the clock two packages down.
package stamp

import "example.com/stamp/clockwrap"

func Stamp(id string) string { return id + "@" + clockwrap.Read() }
