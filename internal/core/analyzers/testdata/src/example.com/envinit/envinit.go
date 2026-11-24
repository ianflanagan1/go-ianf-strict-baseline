// Package envinit reads the environment when it is loaded.
package envinit

import "os"

var home string

func init() { home = os.Getenv("HOME") }

func Home() string { return home }
