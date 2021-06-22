package main

import (
	"fmt"
	"full/ciao"
	"full/hello"
)

var (
	a, b int64
)

func main() {
	a = hello.GetPrivInt64()
	b = ciao.GetLinkedInt64()
	fmt.Printf("hello.privInt64=%d ciao.linkedInt64=%d\n", hello.GetPrivInt64(), ciao.GetLinkedInt64())

	// fmt.Printf("ciao.LinkPrivateStructMethodToFunc=%d\n", ciao.LinkPrivateStructMethodToFunc())

	b = hello.Public{}.GetLinkedInt64()
	fmt.Printf("hello.Public{}.GetLinkedInt64()=%d\n", b)

	ciao.ResolveHelloPrivStructField()
}
