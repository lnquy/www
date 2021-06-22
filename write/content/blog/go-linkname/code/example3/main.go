package main

import _ "unsafe"

// Add/Remove this go:linkname directive and run the command below to see the difference in two cases:
// $ go build -o expl3 main.go && go tool objdump -s 'main.main' expl3
//
//go:linkname age example3/hello.age
var age int64

func main() {
  age = 10
}