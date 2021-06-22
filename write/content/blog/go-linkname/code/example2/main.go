package main

import (
	"fmt"
	"example2/hello"
	_ "unsafe"
)

//go:linkname age example2/hello.age
var age int64

func main() {
  fmt.Println(age)
  age = 10
  fmt.Println(hello.GetAge())
}
