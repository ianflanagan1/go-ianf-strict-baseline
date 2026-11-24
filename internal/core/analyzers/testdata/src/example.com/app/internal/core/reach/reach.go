package reach // want package:"example.com/app/internal/core/reach"

// The core calling third-party code: what it reaches is judged by what that
// code does.

import (
	"example.com/apply"
	"example.com/byname"
	_ "example.com/envchain" // want `importing example.com/envchain runs load-time code that reaches os.Getenv, which is part of package os.*: example.com/envchain → example.com/envinit → example.com/envinit.init → os.Getenv; import only packages whose loading does nothing impure, or move the import to the shell`
	_ "example.com/envinit"  // want `importing example.com/envinit runs load-time code that reaches os.Getenv`
	"example.com/fileutil"
	"example.com/lowlevel"
	"example.com/memo"
	"example.com/printer"
	"example.com/purelib"
	"example.com/shapes"
	"example.com/stamp"
	"example.com/table"
)

func Shout(s string) string { return purelib.Shout(s) }

func Stamp(id string) string { // want Stamp:"reads the clock"
	return stamp.Stamp(id) // want `calling example.com/stamp.Stamp reaches time.Now, which reads the clock: example.com/stamp.Stamp → example.com/stamp/clockwrap.Read → example.com/stamp/clockwrap.now → time.Now; take the value as an argument instead, or move the call to the shell`
}

func Say(s string) string { // want Say:"writes to standard output"
	return printer.Say(s) // want `calling example.com/printer.Say reaches fmt.Println, which writes to standard output`
}

func Square(n int) int { // want Square:"package-level variable"
	return memo.Square(n) // want `calling example.com/memo.Square reaches example.com/memo.cache, which is a package-level variable written outside load-time code`
}

func Ext(name string) string { return fileutil.Ext(name) }

func Read(name string) []byte { // want Read:"part of package os"
	data, err := fileutil.Read(name) // want `calling example.com/fileutil.Read reaches os.ReadFile`
	if err != nil {
		return nil
	}
	return data
}

func One(side int) int { return shapes.One(side) }

func Pick(kind string, side int) int { // want Pick:"writes to standard output"
	return shapes.Pick(kind, side) // want `calling example.com/shapes.Pick reaches fmt.Println, which writes to standard output: example.com/shapes.Pick → \(example.com/shapes.loud\).Area → fmt.Println`
}

type square struct{ side int }

func (s square) Area() int { return s.side * s.side }

func Measure(side int) int { return shapes.Measure(square{side}) }

func Double(xs []int) []int { return apply.Map(xs, double) }

func double(x int) int { return x * 2 }

func Apply(xs []int, f func(int) int) []int { return apply.Map(xs, f) } // want Apply:`\[1\]`

func Echo(xs []int) []int { // want Echo:"writes to standard output"
	return apply.Map(xs, printer.Echo) // want `calling example.com/apply.Map reaches fmt.Println, which writes to standard output: example.com/apply.Map → example.com/printer.Echo → fmt.Println`
}

func Run(name string) int64 { // want Run:"can't be shown pure"
	return table.Run(name) // want `calling example.com/table.Run reaches a function value, which can't be resolved, so can't be shown pure`
}

func Call(v any, name string) { // want Call:"through reflection"
	byname.Call(v, name) // want `calling example.com/byname.Call reaches \(reflect.Value\).MethodByName, which finds a method by name through reflection`
}

func Twice() func(int) int { // want Twice:"writes to standard output"
	return apply.Twice(printer.Echo) // want `calling example.com/apply.Twice reaches fmt.Println, which writes to standard output: example.com/apply.Twice → example.com/printer.Echo → fmt.Println`
}

func TwiceDouble() func(int) int { return apply.Twice(double) }

// A function value another function returns can't be traced.
func CallReturned(x int) int { // want CallReturned:"can't be shown pure"
	return apply.Twice(double)(x) // want `this call reaches a function value, which can't be resolved, so can't be shown pure; take the value as an argument instead, or move the call to the shell`
}

func MeasureNoisy() int { // want MeasureNoisy:"writes to standard output"
	return apply.Measure(shapes.Noisy{}) // want `calling example.com/apply.Measure reaches fmt.Println, which writes to standard output: example.com/apply.Measure → \(example.com/shapes.Noisy\).Area → fmt.Println`
}

func MeasureSquare(side int) int { return apply.Measure(square{side}) }

func Untraced(name string) int { // want Untraced:"writes to standard output"
	return shapes.Untraced(name) // want `calling example.com/shapes.Untraced reaches fmt.Println`
}

func Local(name string) int64 { // want Local:"reads the clock"
	return table.Local(name) // want `calling example.com/table.Local reaches time.Now, which reads the clock: example.com/table.Local → example.com/table.now → time.Now`
}

func PureTable(name string) int64 { return table.Pure(name) }

func Missing(err error) bool { return fileutil.Missing(err) } // want Missing:`\[0\]`

func Trace(s string) string { // want Trace:"standard error"
	return lowlevel.Trace(s) // want `calling example.com/lowlevel.Trace reaches println, which writes to standard error`
}

func Bytes(s string) []byte { // want Bytes:"type system"
	return lowlevel.Bytes(s) // want `calling example.com/lowlevel.Bytes reaches unsafe.StringData, which is an operation the type system can't check`
}

func Zone() string { // want Zone:"local time zone"
	return lowlevel.Zone() // want `calling example.com/lowlevel.Zone reaches time.Local, which reads the machine's local time zone`
}
