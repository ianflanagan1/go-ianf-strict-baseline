// Package printer writes to standard output.
package printer

import "fmt"

func Say(s string) string {
	fmt.Println(s)
	return s
}

func Echo(n int) int {
	fmt.Println(n)
	return n
}
