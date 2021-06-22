package main

import (
	"fmt"
	"example1/hello"
	_ "unsafe"
)

//go:linkname linkedMsg example1/hello.msg
var linkedMsg string

func main() {
  fmt.Println(hello.Message) // "Exported message"
  fmt.Println(linkedMsg)     // "private message"
}