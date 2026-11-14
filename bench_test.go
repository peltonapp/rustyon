// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
)

// benchItem and benchInventory model a typical configuration or save file:
// nested structs, slices, maps, strings and numbers. The same Go values are
// used for both rustyon and encoding/json so the numbers are comparable.
type benchItem struct {
	ID     int            `json:"id"`
	Name   string         `json:"name"`
	Weight float64        `json:"weight"`
	Tags   []string       `json:"tags"`
	Stats  map[string]int `json:"stats"`
	Owner  *string        `json:"owner"`
}

type benchInventory struct {
	Version int         `json:"version"`
	Title   string      `json:"title"`
	Items   []benchItem `json:"items"`
}

func makeInventory(n int) benchInventory {
	owner := "ferris"
	inv := benchInventory{Version: 3, Title: "Inventory", Items: make([]benchItem, n)}
	for i := range inv.Items {
		var o *string
		if i%2 == 0 {
			o = &owner
		}
		inv.Items[i] = benchItem{
			ID:     i,
			Name:   "item number " + strconv.Itoa(i),
			Weight: float64(i) * 0.25,
			Tags:   []string{"common", "tradeable", "stackable"},
			Stats:  map[string]int{"attack": i % 17, "defense": i % 13},
			Owner:  o,
		}
	}
	return inv
}

func BenchmarkMarshal(b *testing.B) {
	inv := makeInventory(100)
	b.Run("rustyon", func(b *testing.B) {
		out, _ := Marshal(inv)
		b.SetBytes(int64(len(out)))
		b.ReportAllocs()
		for b.Loop() {
			if _, err := Marshal(inv); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("json", func(b *testing.B) {
		out, _ := json.Marshal(inv)
		b.SetBytes(int64(len(out)))
		b.ReportAllocs()
		for b.Loop() {
			if _, err := json.Marshal(inv); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkUnmarshal(b *testing.B) {
	inv := makeInventory(100)
	ronData, _ := Marshal(inv)
	jsonData, _ := json.Marshal(inv)
	b.Run("rustyon", func(b *testing.B) {
		b.SetBytes(int64(len(ronData)))
		b.ReportAllocs()
		for b.Loop() {
			var out benchInventory
			if err := Unmarshal(ronData, &out); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("json", func(b *testing.B) {
		b.SetBytes(int64(len(jsonData)))
		b.ReportAllocs()
		for b.Loop() {
			var out benchInventory
			if err := json.Unmarshal(jsonData, &out); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkDynamic compares parsing into a dynamic tree: rustyon's Value
// against encoding/json decoding into any.
func BenchmarkDynamic(b *testing.B) {
	inv := makeInventory(100)
	ronData, _ := Marshal(inv)
	jsonData, _ := json.Marshal(inv)
	b.Run("rustyon", func(b *testing.B) {
		b.SetBytes(int64(len(ronData)))
		b.ReportAllocs()
		for b.Loop() {
			if _, err := Parse(ronData); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("json", func(b *testing.B) {
		b.SetBytes(int64(len(jsonData)))
		b.ReportAllocs()
		for b.Loop() {
			var v any
			if err := json.Unmarshal(jsonData, &v); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkFormat compares Format against json.Indent on compact input.
func BenchmarkFormat(b *testing.B) {
	inv := makeInventory(100)
	ronData, _ := Marshal(inv)
	jsonData, _ := json.Marshal(inv)
	b.Run("rustyon", func(b *testing.B) {
		b.SetBytes(int64(len(ronData)))
		b.ReportAllocs()
		for b.Loop() {
			if _, err := Format(ronData); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("json", func(b *testing.B) {
		b.SetBytes(int64(len(jsonData)))
		b.ReportAllocs()
		var buf bytes.Buffer
		for b.Loop() {
			buf.Reset()
			if err := json.Indent(&buf, jsonData, "", "    "); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkScaling measures how the cost grows with input size. Linear
// growth shows up as a constant ns per byte across sizes.
func BenchmarkScaling(b *testing.B) {
	for _, n := range []int{10, 100, 1000, 10000} {
		inv := makeInventory(n)
		data, _ := Marshal(inv)
		b.Run(fmt.Sprintf("Unmarshal/items=%d", n), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				var out benchInventory
				if err := Unmarshal(data, &out); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("Marshal/items=%d", n), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if _, err := Marshal(inv); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("Format/items=%d", n), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if _, err := Format(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
