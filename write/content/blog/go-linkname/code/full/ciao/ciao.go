package ciao

import (
	"fmt"
	"reflect"
	_ "unsafe"
)

////go:linkname linkedInt64 full/hello.privInt64
var linkedInt64 int64 = 10

//go:noinine
func GetLinkedInt64() int64 {
	return linkedInt64
}

// //go:linkname LinkPrivateStructMethodToFunc full/hello.Public.getPrivInt64
// func LinkPrivateStructMethodToFunc() int64

//go:linkname getLinkedInt64 full/hello.Public.GetLinkedInt64
func getLinkedInt64() int64 {
	return linkedInt64
}

type copiedPrivStruct struct {
	field int64
}

//go:linkname getHelloPrivStruct full/hello.getPrivStruct
func getHelloPrivStruct() *copiedPrivStruct

func ResolveHelloPrivStructField() int64 {
	p := getHelloPrivStruct()
	fmt.Println(p.field)
	v := reflect.ValueOf(p)
	fmt.Printf("%#v", v.FieldByName("field").Int())
	return 0
}
