---
title: "//go:linkname"
description: "Everything you need to know about go:linkname directive."
lead: ""
date: 2022-02-22T22:58:40+07:00
lastmod: 2022-02-22T22:58:40+07:00
draft: false
weight: 50
images: ["go-linkname.jpg"]
contributors: ["quy-le"]
toc: true
categories: []
tags: ['go', 'golang', 'linkname', 'compiler', 'pragma']
---

### Compiler directives

In Go programming language, to permit access to an indentifier (variable, struct's fields/methods, functions...) from another package, you must export it [^1] by changing the first character of the identifier to UPPERCASE.  
For example:

```go
// -- example1/hello/hello.go --
package hello

var msg = "private message"      // msg can only be called/used inside the package `hello`.
var Message = "Exported message" // Message can be accessed from another package by `hello.Message`.


// -- example1/main.go --
package main

import (
  "fmt"
  "my-module/hello"
)

func main() {
  fmt.Println(hello.Message) // "Exported message"
  fmt.Println(hello.msg)     // Compile error, `msg` is not exported so it's inaccessable outside of package `hello`.
}
```

This is basic knowledge in Go, nothing special here.  

And what if I tell you there is a way for the `main` package to access the unexported `hello.msg` variable?  

```go
// -- example1/hello/hello.go --
package hello

var msg = "private message"
var Message = "Exported message"


// -- example1/main.go --
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

// On Go playground: https://play.golang.org/p/8Yew6Hh50Gt  
```

You can compile and run the example above without error 

```shell
$ go run main.go 
Exported message
private message
```

We can see the `private message` printed out, which means `main` package can access to the unexported `hello.msg` variable now. That is because of the `//go:linkname` directive at line #17.  

`go:linkname` (along with some others, e.g.: `go:noescape`, `go:inline`, `go:norace`...) are compiler directives or commonly known as pragmas that we can use to tell Go compiler to do something different/special than normal compiling. The general form of Go directives is:

```go
//go:directive [params]
```
Note that there's no space in between the double slashes and the `go:directive` keyword, this is because of legacy but it also helps distinguish the directives with normal comments.  

### Caution

1. Compiler directives/pragmas are not a part of the Go programming language. It's implemented in the compiler but with no guarantee that the directive behavior won't be changed or even removed in the future.  
   Practically, you would never need to use these directives in user code. But while reading Go source code, you will see these directives quite frequently so it's nice to understand.  
   And maybe it can be used (with caution in mind, of course) to do clever things as I will show you in the end of this post.
2. I only cover `go:linkname` directive in this post.  
   For other directives, I cannot recommend more that you should go read [the official Godoc](https://golang.org/cmd/compile/#hdr-Compiler_Directives) and [this wonderful post](https://dave.cheney.net/2018/01/08/gos-hidden-pragmas) from Dave Cheney where he explained in details the historical purposes and usage of these directives.

### //go:linkname

```go
//go:linkname localname [importpath.name]
```
> The //go:linkname directive instructs the compiler to use “importpath.name” as the object file symbol name for the variable or function declared as “localname” in the source code.  
> [...] Because this directive can subvert the type system and package modularity, it is only enabled in files that have imported "unsafe".  
> \- [Godoc](https://golang.org/cmd/compile/#hdr-Compiler_Directives)

In another explanation, whenever seeing `go:linkname` directive, Go compiler will replace all the `localname` in source code by a direct reference to the `importpath.name`.  
The reference is defined in the compiled binary so it doesn't matter if the `importpath.name` is exported in source code or not, we can always access it directly (more about this in below section).

- `go:linkname` can be used to reference to an unexported (private) variable or function.
- `go:linkname` can beplaced on the either the source or destination package and don't have to be placed right above the linking variable/function. But should place right above for better tracking and clarity. 
- Must import `unsafe` package.
- The package where `go:linkname` directive is defined must be imported.

```go
// -- go.mod --
module expl


// -- hello/hello.go --
package hello

import (
	_ "unsafe"
)

// linkname directive don't have to be directly above the linking source/destination.
//go:linkname privBytes expl/ciao.linkedBytes
var privInt64 int64 = 5
var privBytes []byte = []byte("hello")

func privAddFunc(a int64) int64 {
	return a + privInt64
}

func GetInt64() int64 { return privInt64 }
func GetBytes() []byte { return privBytes }


// -- ciao/ciao.go --
package ciao

import (
	_ "expl/hello"
	_ "unsafe"
)

var (
  // linkname directive can be placed on either source or destination package.
	//go:linkname linkedInt64 expl/hello.privInt64
	linkedInt64 int64
	linkedBytes []byte
)

// Link to private function
//go:linkname LinkedAddFunc expl/hello.privAddFunc
func LinkedAddFunc(a int64) int64

func GetInt64() int64 { return linkedInt64 }
func GetBytes() []byte { return linkedBytes }


// -- main.go --
package main

import (
  "fmt"
	"expl/ciao"
	"expl/hello"
)

func main() {
	fmt.Printf("hello.privInt64=%d, ciao.linkedInt64=%d\n", hello.GetInt64(), ciao.GetInt64())   // 5
	fmt.Printf("hello.privBytes=%s, ciao.linkedBytes=%s\n", hello.GetBytes(), ciao.GetBytes())   // "hello"
	fmt.Printf("ciao.linkedAddFunc(10)=%d\n", ciao.LinkedAddFunc(10))   // 15
}

// On Go playground: https://play.golang.org/p/xwGIIx97x7B
```

We can verify the linkname reference by two ways as below:

##### 1. Verify linkname reference by Go code

```go
// -- example2/hello/hello.go --
package hello

var age int64 = 5

func GetAge() int64 {
	return age
}


// -- example2/main.go --
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
```

Run the example2 above:

```shell
$ go run main.go
5
10
```

We can see that in the code we only change the `main.age` but that change also refect to the `hello.age`, which suggest linkname is a reference.

##### 2. Verify linkname reference by compiled Assembly

```go
// -- example3/main.go --
package main

var age int64

func main() {
  age = 10
}
```

```shell
$ go build -o expl3 main.go && go tool objdump -s 'main.main' expl3 
TEXT main.main(SB) example3/main.go
  main.go:7             0x104ea20               48c705edd008000a000000  MOVQ $0xa, main.age(SB)
  main.go:8             0x104ea2b               c3                      RET
```

At line #3, we see the instruction to set 10 (0xA) to the `main.age` variable which has the address of `0x0104ea20`. Nothing special here.

Now try to use go:linkname to link the `hello.age` variable to `main.age` and see what's the difference in Assembly code.

```go
// -- example3/main.go --
package main

import _ "unsafe"

//go:linkname age example3/hello.age
var age int64

func main() {
  age = 10
}

// -- example3/hello/hello.go --
package hello

var age int64 = 5
```

```shell
$ go build -o expl3 main.go && go tool objdump -s 'main.main' expl3
TEXT main.main(SB) example3/main.go
  main.go:9             0x104ea20               48c705e5d008000a000000  MOVQ $0xa, example3/hello.age(SB)
  main.go:10            0x104ea2b               c3                      RET
```

At line #3, we can see that now the `main.age = 10` code has been translated directly to `example3/hello.age = 10`.  
In the code we did define the `main.age` variable, but searching in the compiled file, I couldn't locate the location of `main.age` anymore. And along with the Assembly code above suggest that all the `main.age` accesses are now referenced directly to the `example3/hello.age` variable.  
Which explained why on the Go code we only changed the local variable (`main.age`) but it also reflect to the original variable (`example3/hello.age`).

### Exceptional uses

- Place the `go:linkname` at source or destination file doesn't change the linking behavior, but on the compiled code level, it's different. `go:linkname` always replace the `localname` by the `impothpath.name` reference.  

  Place the `go:linkname` in the destination package (`ciao`).

  ```go
  // -- hello/hello.go --
  var privInt64 int64 = 5
  
  // -- ciao/ciao.go --
  //go:linkname linkedInt64 full/hello.privInt64
  var linkedInt64 int64
  
  // -- main.go --
  func main() {	
  	a = hello.GetPrivInt64()
  	b = ciao.GetLinkedInt64()
  }
  
  
  $ go build -o c3 main.go && go tool objdump -s 'main.main' c3
  TEXT main.main(SB) /Users/quy.l/ws/projects/go/modules/www/write/content/blog/go-linkname/code/full/main.go
    main.go:13            0x105e180               488b0501a00600          MOVQ full/hello.privInt64(SB), AX
    main.go:13            0x105e187               4889057aa30900          MOVQ AX, main.a(SB)
    main.go:14            0x105e18e               488b05f39f0600          MOVQ full/hello.privInt64(SB), AX
    main.go:14            0x105e195               48890574a30900          MOVQ AX, main.b(SB)
    main.go:15            0x105e19c               c3                      RET
  ```

  Place the `go:linkname` in the source package (`hello`).

  ```go
  // -- hello/hello.go --
  //go:linkname privInt64 full/ciao.linkedInt64
  var privInt64 int64 = 5
  
  // -- ciao/ciao.go --
  var linkedInt64 int64
  
  // -- main.go --
  func main() {	
  	a = hello.GetPrivInt64()
  	b = ciao.GetLinkedInt64()
  }
  
  
  $ go build -o c3 main.go && go tool objdump -s 'main.main' c3
  TEXT main.main(SB) /Users/quy.l/ws/projects/go/modules/www/write/content/blog/go-linkname/code/full/main.go
    main.go:13            0x105e180               488b0501a00600          MOVQ full/ciao.linkedInt64(SB), AX
    main.go:13            0x105e187               4889057aa30900          MOVQ AX, main.a(SB)
    main.go:14            0x105e18e               488b05f39f0600          MOVQ full/ciao.linkedInt64(SB), AX
    main.go:14            0x105e195               48890574a30900          MOVQ AX, main.b(SB)
    main.go:15            0x105e19c               c3                      RET
  ```

- Should only place `go:linkname` on either source or destination package, not both. Declare `go:linkname` on both two packages still works as intended but it's easy to get lost.

- Initialize the variable/function on both 2 places and also link it causes compile fail with `duplicated definition of symbol`.

- You can actually link to or from the struct's private methods too, but it's easy to get SEGFAULT, so do not try it.

|      | hello/hello.go                                               | ciao/ciao.go                                                 | stdout                                                       | Assembly                                                     |
| ---- | ------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------ |
| 1    | var privInt64 int64 = 5                                      | var linkedInt64 int64                                        | hello.privInt64=5 ciao.linkedInt64=0                         | go build -o c3 main.go && go tool objdump -s 'main.main' c3<br>TEXT main.main(SB) /Users/quy.l/ws/projects/go/modules/www/write/content/blog/go-linkname/code/full/main.go<br>  main.go:13            0x105e180               488b0501a00600          MOVQ full/hello.privInt64(SB), AX<br>  main.go:13            0x105e187               48890582a30900          MOVQ AX, main.a(SB)<br>  main.go:14            0x105e18e               488b056ba30900          MOVQ full/ciao.linkedInt64(SB), AX<br>  main.go:14            0x105e195               4889057ca30900          MOVQ AX, main.b(SB)<br>  main.go:15            0x105e19c               c3                      RET |
| 2    | var privInt64 int64 = 5                                      | //go:linkname linkedInt64 full/hello.privInt64<br>var linkedInt64 int64 | hello.privInt64=5 ciao.linkedInt64=5                         | go build -o c3 main.go && go tool objdump -s 'main.main' c3<br>TEXT main.main(SB) /Users/quy.l/ws/projects/go/modules/www/write/content/blog/go-linkname/code/full/main.go<br>  main.go:13            0x105e180               488b0501a00600          MOVQ full/hello.privInt64(SB), AX<br>  main.go:13            0x105e187               4889057aa30900          MOVQ AX, main.a(SB)<br>  main.go:14            0x105e18e               488b05f39f0600          MOVQ full/hello.privInt64(SB), AX<br>  main.go:14            0x105e195               48890574a30900          MOVQ AX, main.b(SB)<br>  main.go:15            0x105e19c               c3                      RET |
| 3    | //go:linkname privInt64 full/ciao.linkedInt64<br>var privInt64 int64 = 5 | var linkedInt64 int64                                        | hello.privInt64=5 ciao.linkedInt64=5                         | go build -o c3 main.go && go tool objdump -s 'main.main' c3<br>TEXT main.main(SB) /Users/quy.l/ws/projects/go/modules/www/write/content/blog/go-linkname/code/full/main.go<br>  main.go:13            0x105e180               488b0501a00600          MOVQ full/ciao.linkedInt64(SB), AX<br>  main.go:13            0x105e187               4889057aa30900          MOVQ AX, main.a(SB)<br>  main.go:14            0x105e18e               488b05f39f0600          MOVQ full/ciao.linkedInt64(SB), AX<br>  main.go:14            0x105e195               48890574a30900          MOVQ AX, main.b(SB)<br>  main.go:15            0x105e19c               c3                      RET |
| 4    | var privInt64 int64 = 5                                      | //go:linkname linkedInt64 full/hello.privInt64<br>var linkedInt64 int64 = 10 | Compile error: duplicated definition of symbol full/hello.privInt64 |                                                              |
| 5    | //go:linkname privInt64 full/ciao.linkedInt64<br>var privInt64 int64 = 5 | var linkedInt64 int64 = 10                                   | Compile error: duplicated definition of symbol full/ciao.linkedInt64 |                                                              |
| 6    | //go:linkname privInt64 full/ciao.linkedInt64<br>var privInt64 int64 = 5 | //go:linkname linkedInt64 full/hello.privInt64<br>var linkedInt64 int64 | hello.privInt64=5 ciao.linkedInt64=0                         | go build -o c3 main.go && go tool objdump -s 'main.main' c3<br>TEXT main.main(SB) /Users/quy.l/ws/projects/go/modules/www/write/content/blog/go-linkname/code/full/main.go<br>  main.go:14            0x105e180               488b0501a00600          MOVQ full/ciao.linkedInt64(SB), AX<br>  main.go:14            0x105e187               48890582a30900          MOVQ AX, main.a(SB)<br>  main.go:15            0x105e18e               488b056ba30900          MOVQ full/hello.privInt64(SB), AX<br>  main.go:15            0x105e195               4889057ca30900          MOVQ AX, main.b(SB)<br>  main.go:18            0x105e19c               c3                      RET |
| 7    | //go:linkname privInt64 full/ciao.linkedInt64<br>var privInt64 int64 | //go:linkname linkedInt64 full/hello.privInt64<br>var linkedInt64 int64 = 10 | hello.privInt64=0 ciao.linkedInt64=10                        | go build -o c3 main.go && go tool objdump -s 'main.main' c3<br>TEXT main.main(SB) /Users/quy.l/ws/projects/go/modules/www/write/content/blog/go-linkname/code/full/main.go<br>  main.go:14            0x105e180               488b0579a30900          MOVQ full/ciao.linkedInt64(SB), AX<br>  main.go:14            0x105e187               48890582a30900          MOVQ AX, main.a(SB)<br>  main.go:15            0x105e18e               488b05f39f0600          MOVQ full/hello.privInt64(SB), AX<br>  main.go:15            0x105e195               4889057ca30900          MOVQ AX, main.b(SB)<br>  main.go:18            0x105e19c               c3                      RET |
| 8    | //go:linkname privInt64 full/ciao.linkedInt64<br>var privInt64 int64 = 5 | //go:linkname linkedInt64 full/hello.privInt64<br>var linkedInt64 int64 = 10 | hello.privInt64=5 ciao.linkedInt64=10                        | go build -o c3 main.go && go tool objdump -s 'main.main' c3<br>TEXT main.main(SB) /Users/quy.l/ws/projects/go/modules/www/write/content/blog/go-linkname/code/full/main.go<br>  main.go:14            0x105e180               488b0501a00600          MOVQ full/ciao.linkedInt64(SB), AX<br>  main.go:14            0x105e187               4889057aa30900          MOVQ AX, main.a(SB)<br>  main.go:15            0x105e18e               488b05fb9f0600          MOVQ full/hello.privInt64(SB), AX<br>  main.go:15            0x105e195               48890574a30900          MOVQ AX, main.b(SB)<br>  main.go:18            0x105e19c               c3                      RET |
| 9    | type Public struct {}<br><br>func (p Public) getPrivInt64() int64 {<br> return privInt64<br>} | //go:linkname LinkPrivateStructMethodToFunc full/hello.Public.getPrivInt64<br>func LinkPrivateStructMethodToFunc() int64 | hello.privInt64=5 ciao.linkedInt64=0<br>ciao.LinkPrivateStructMethodToFunc=5 | go build -o c3 main.go && go tool objdump -s 'Public.getPrivInt64' c3<br>TEXT full/hello.Public.getPrivInt64(SB) /Users/quy.l/ws/projects/go/modules/www/write/content/blog/go-linkname/code/full/hello/hello.go<br>  hello.go:20           0x105e180               488b0501a00600          MOVQ full/hello.privInt64(SB), AX<br>  hello.go:20           0x105e187               4889442408              MOVQ AX, 0x8(SP)<br>  hello.go:20           0x105e18c               c3                      RET |
| 10   | type Public struct {<br> field string<br>}<br><br>func (p Public) getPrivInt64() int64 {<br> return privInt64<br>} | //go:linkname LinkPrivateStructMethodToFunc full/hello.Public.getPrivInt64<br>func LinkPrivateStructMethodToFunc() int64 | SEGFAULT:<br>hello.privInt64=5 ciao.linkedInt64=0<br>ciao.LinkPrivateStructMethodToFunc=17733920 | go build -o c3 main.go && go tool objdump -s 'Public.getPrivInt64' c3<br>TEXT full/hello.Public.getPrivInt64(SB) /Users/quy.l/ws/projects/go/modules/www/write/content/blog/go-linkname/code/full/hello/hello.go<br>  hello.go:21           0x105e180               488b0501a00600          MOVQ full/hello.privInt64(SB), AX<br>  hello.go:21           0x105e187               4889442418              MOVQ AX, 0x18(SP)<br>  hello.go:21           0x105e18c               c3                      RET |
| 11   | var linkedInt64 int64 = 10<br><br>type Public struct {<br> field string<br>}<br><br>func (p Public) GetLinkedInt64() int64 | //go:linkname getLinkedInt64 full/hello.Public.GetLinkedInt64<br>//go:noinline<br>func getLinkedInt64() int64 {<br> return linkedInt64<br>} | SEGFAULT:<br>hello.Public{}.GetLinkedInt64()=0               | go build -o c3 main.go && go tool objdump -s 'Public.GetLinkedInt64' c3<br>TEXT full/hello.Public.GetLinkedInt64(SB) /Users/quy.l/ws/projects/go/modules/www/write/content/blog/go-linkname/code/full/ciao/ciao.go<br>  ciao.go:21            0x10a4600               488b05416c0a00          MOVQ full/ciao.linkedInt64(SB), AX<br>  ciao.go:21            0x10a4607               4889442408              MOVQ AX, 0x8(SP)<br>  ciao.go:21            0x10a460c               c3                      RET |

### Clever uses

##### 1. Read the value of unexported struct's private field

```go
// -- hello/hello.go --
package hello 

// Objective: From another package, how can we read the value of privStruct.field?
type privStruct struct {
	field int64
}

func getPrivStruct() *privStruct {
	return &privStruct{ field: 100 }
}

// -- ciao/ciao.go --
package ciao

import (
	_ "unsafe"
)

// This struct is the copied of hello/hello.privStruct
type copiedPrivStruct struct {
	field int64
}

//go:linkname getHelloPrivStruct full/hello.getPrivStruct
func getHelloPrivStruct() *copiedPrivStruct

func ResolveHelloPrivStructField() int64 {
	p := getHelloPrivStruct()
	return p.field // We can read the 100 value from hello.getPrivStruct here without error
}
```

The idea of this usage came from the process of somehow we can read the goroutine's ID. If we can `go:linkname` the `runtime.getg()` function which returns the current goroutine `g`. Inside the `g` struct, there is a `goid` field which is the goroutine ID.  
Note: This approach is failed because we cannot link `runtime.getg()`, because that function itself has been implemented/linked from the Assembly directly.  
Take a look at [this package](https://github.com/modern-go/gls) when the author have to implement `getg` function in Assembly instead.

##### 2. Access to unexported TLS 1.3 cipher suites

The author of [this post](https://www.joeshaw.org/abusing-go-linkname-to-customize-tls13-cipher-suites/) want to write a tool to scan all the supported TLS ciphers of server but Go's `crypto/tls` package doesn't expose the list of default cipher suites for TLS 1.3.  
So he used `go:linkname` to have access to the unexported list.

##### 3. Access to runtime function to trace goroutine's allocated memory

We can monitor the process memory in Go, but for occasional fine tuning, we may want to track the number of heap allocated bytes, objects or calls per goroutine.  
In the way to achive that goal, the author of [this post]() had to use `go:linkname` to get access to the unexported `mallocgc` function from `runtime` package.

[^1]: Go spec - Exported Identifiers: [https://golang.org/ref/spec#Exported_identifiers](https://golang.org/ref/spec#Exported_identifiers).

<script type="text/javascript">
  document.getElementsByTagName("table")[0].classList.add("long-table");

  let highlightLine = document.querySelector("code.language-go span[id='18'] a");
  highlightLine.classList.add("error");
</script>
