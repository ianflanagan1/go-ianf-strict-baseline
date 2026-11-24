// Package byname calls a method by name through reflection.
package byname

import "reflect"

func Call(v any, name string) {
	reflect.ValueOf(v).MethodByName(name).Call(nil)
}
