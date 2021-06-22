package hello

import (
	_ "unsafe"
)

////go:linkname privInt64 full/ciao.linkedInt64
var privInt64 int64 = 5

//go:noinine
func GetPrivInt64() int64 {
	return privInt64
}

type Public struct {
	field string
}

func (p Public) getPrivInt64() int64 {
	return privInt64
}

func (p Public) GetLinkedInt64() int64

type privStruct struct {
	field int64
}

// From ciao package, we want to able to get the value of privStruct.field
func getPrivStruct() *privStruct {
	return &privStruct{
		field: 100,
	}
}
