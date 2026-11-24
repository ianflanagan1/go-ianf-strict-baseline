// Package memo keeps a cache in a package-level map.
package memo

var cache = map[int]int{}

func Square(n int) int {
	if v, ok := cache[n]; ok {
		return v
	}
	v := n * n
	cache[n] = v
	return v
}
