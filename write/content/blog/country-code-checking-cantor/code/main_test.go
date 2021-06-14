package main

import (
	"fmt"
	"testing"
)

var _tmp bool

func BenchmarkCheckCountryCode(b *testing.B) {
	formatName := func(s string) string {
		return fmt.Sprintf("%14s", s)
	}

	funcs := []struct {
		name string
		f    func(string) bool
	}{
		{name: formatName("array_naive"), f: IsCountryCodeByArray},
		{name: formatName("map_string"), f: IsCountryCodeByMapString},
		// {name: formatName("map_number"), f: IsCountryCodeByMapInt},
		{name: formatName("cantor_pairing"), f: IsCountryCodeByCantorPairing},
		{name: formatName("cantor_bitmap"), f: IsCountryCodeByCantorBitmap},
	}

	for _, benchFunc := range funcs {
		b.Run(benchFunc.name+"_hit", func(b *testing.B) {
			// b.SetParallelism(4)
			for i := 0; i < b.N; i++ {
				_tmp = benchFunc.f("ad")
			}
		})
	}

	for _, benchFunc := range funcs {
		b.Run(benchFunc.name+"_miss", func(b *testing.B) {
			// b.SetParallelism(4)
			for i := 0; i < b.N; i++ {
				_tmp = benchFunc.f("zz")
			}
		})
	}
}

func TestIsCountryCodeByCantorPairing(t *testing.T) {
	for c1 := 'a'; c1 <= 'z'; c1++ {
		for c2 := 'a'; c2 <= 'z'; c2++ {
			input := string(c1) + string(c2)
			got := IsCountryCodeByCantorPairing(input)
			if expected := IsCountryCodeByMapString(input); got != expected {
				t.Errorf("IsCountryCodeByCantorPairing(%q)=%v, expected %v", input, got, expected)
				return
			}
		}
	}
}

func TestCantorUniqueness(t *testing.T) {
	m := make(map[int]string, 26*26)

	for c1 := 'a'; c1 <= 'z'; c1++ {
		for c2 := 'a'; c2 <= 'z'; c2++ {
			x, y := int(c1-97), int(c2-97)
			cantor := detailCantor(x, y)
			rcX, rcY := reverseDetailCantor(cantor)
			if rcX != x || rcY != y {
				t.Errorf("cantor and reversed not match: cantor(%d, %d)=%d, reverse(%d)=[%d, %d]", x, y, cantor, cantor, rcX, rcY)
				return
			}

			v, ok := m[cantor]
			if !ok {
				m[cantor] = string(c1) + string(c2)
				continue
			}
			t.Errorf("found same cantor pair: c(%s)=%d, c(%s)=%d", v, cantor, string(c1)+string(c2), cantor)
			return
		}
	}
	t.Logf("%v", m)
}
