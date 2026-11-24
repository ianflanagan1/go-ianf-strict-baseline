// Package shapes calls methods on interface values it builds itself.
package shapes

import "fmt"

type Shape interface{ Area() int }

type square struct{ side int }

func (s square) Area() int { return s.side * s.side }

type loud struct{ side int }

func (l loud) Area() int {
	fmt.Println("area of", l.side)
	return l.side * l.side
}

// One builds only a square, so only square.Area is reached, though loud is
// another Shape.
func One(side int) int {
	var s Shape = square{side}
	return s.Area()
}

// Pick chooses which Shape to build from a value it reads at run time, so
// the call reaches both.
func Pick(kind string, side int) int {
	var s Shape = square{side}
	if kind == "loud" {
		s = loud{side}
	}
	return s.Area()
}

// Measure calls a method on a value its caller passes.
func Measure(s Shape) int { return s.Area() }

// Noisy is a Shape that prints.
type Noisy struct{}

func (Noisy) Area() int {
	fmt.Println("noisy")
	return 0
}

// FromName returns a Shape another function builds.
func FromName(name string) Shape {
	if name == "noisy" {
		return Noisy{}
	}
	return square{1}
}

// Untraced calls a method on a value another function returned, which can
// be any Shape the package can see.
func Untraced(name string) int { return FromName(name).Area() }
