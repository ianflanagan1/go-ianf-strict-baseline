package analyzers

import (
	"go/types"
	"strings"
)

// operations maps the standard library's impure operations to what each
// does. It is the only list CoreReachesNothingImpure judges by, and it names
// standard-library functions, methods, variables and packages, never a
// third-party or first-party package: those are judged by their code.
type operations struct {
	// named maps a function, method or variable, by its full name as
	// types.Func.FullName or ssa.Global.String gives it, to what it does.
	named map[string]string
	// packages maps a package, with every package under it, to what each
	// of its functions, methods and variables does.
	packages map[string]string
	// pure are packages under one in packages that do nothing impure.
	pure []string
}

func impureOperations() operations {
	const (
		clock    = "reads the clock"
		timer    = "starts or changes a timer"
		local    = "reads the machine's local time zone"
		stdout   = "writes to standard output"
		reflects = "finds a method by name through reflection, which can't be followed"
		files    = "reads the file system"
		runtime  = "reads or changes the Go runtime's state"
		mime     = "reads or changes the system's MIME tables"
		random   = "draws a random number"
	)
	return operations{
		named: map[string]string{
			"time.Now": clock, "time.Since": clock, "time.Until": clock,
			"time.Sleep": "sleeps", "time.Tick": timer, "time.After": timer, "time.AfterFunc": timer,
			"time.NewTimer": timer, "time.NewTicker": timer,
			"(*time.Timer).Reset": timer, "(*time.Timer).Stop": timer,
			"(*time.Ticker).Reset": timer, "(*time.Ticker).Stop": timer,
			"time.Local": local, "time.LoadLocation": local, "time.Unix": local, "time.UnixMilli": local,
			"time.UnixMicro": local, "(time.Time).Local": local,
			"fmt.Print": stdout, "fmt.Printf": stdout, "fmt.Println": stdout,
			"(reflect.Value).MethodByName": reflects, "(reflect.Value).Method": reflects,
			"(reflect.Value).Call": reflects, "(reflect.Value).CallSlice": reflects,
			"(reflect.Type).MethodByName": reflects, "(reflect.Type).Method": reflects,
			"path/filepath.Abs": files, "path/filepath.EvalSymlinks": files, "path/filepath.Glob": files,
			"path/filepath.Walk": files, "path/filepath.WalkDir": files,
			"text/template.ParseFiles": files, "text/template.ParseGlob": files,
			"(*text/template.Template).ParseFiles": files, "(*text/template.Template).ParseGlob": files,
			"html/template.ParseFiles": files, "html/template.ParseGlob": files,
			"(*html/template.Template).ParseFiles": files, "(*html/template.Template).ParseGlob": files,
			"runtime.NumCPU": runtime, "runtime.GOMAXPROCS": runtime, "runtime.NumGoroutine": runtime,
			"runtime.Caller": runtime, "runtime.Callers": runtime, "runtime.GC": runtime,
			"runtime.ReadMemStats": runtime, "runtime.SetFinalizer": runtime, "runtime.GOROOT": runtime,
			"hash/maphash.MakeSeed": random,
			"mime.TypeByExtension":  mime, "mime.ExtensionsByType": mime, "mime.AddExtensionType": mime,
		},
		packages: map[string]string{
			"os":           "is part of package os (files, the environment, processes and the standard streams)",
			"syscall":      "makes a system call",
			"net":          "uses the network",
			"plugin":       "loads a plugin",
			"crypto/rand":  random,
			"math/rand":    random,
			"log":          "writes to a log",
			"database/sql": "uses a database",
			"io/ioutil":    "reads or writes files",
		},
		pure: []string{"net/mail", "net/netip", "net/url"},
	}
}

// ofFunc returns what the standard-library function or method fn does, and
// false when it does nothing impure. A method of an error type in an impure
// package, such as (*os.PathError).Error, only describes a failure, and is
// pure.
func (o operations) ofFunc(fn *types.Func) (string, bool) {
	if op, ok := o.named[fn.FullName()]; ok {
		return op, true
	}
	if fn.Pkg() == nil {
		return "", false
	}
	if recv := fn.Signature().Recv(); recv != nil && isErrorType(recv.Type()) {
		return "", false
	}
	return o.ofPackage(fn.Pkg().Path())
}

// ofVar returns what reading the standard-library variable named name, of
// type t in package path, does, and false when it does nothing impure. A
// sentinel error, such as os.ErrNotExist, is pure.
func (o operations) ofVar(name, path string, t types.Type) (string, bool) {
	if op, ok := o.named[name]; ok {
		return op, true
	}
	if isErrorType(t) {
		return "", false
	}
	return o.ofPackage(path)
}

func (o operations) ofPackage(path string) (string, bool) {
	for _, pure := range o.pure {
		if path == pure || strings.HasPrefix(path, pure+"/") {
			return "", false
		}
	}
	for pkg, op := range o.packages {
		if path == pkg || strings.HasPrefix(path, pkg+"/") {
			return op, true
		}
	}
	return "", false
}

// isErrorType reports whether t, or a pointer to it, implements error.
func isErrorType(t types.Type) bool {
	errorType := types.Universe.Lookup("error").Type()
	value := dereferenced(t)
	return types.AssignableTo(value, errorType) || types.AssignableTo(types.NewPointer(value), errorType)
}
