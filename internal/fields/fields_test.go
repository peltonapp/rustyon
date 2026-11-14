// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package fields

import (
	"reflect"
	"testing"
)

func TestSnakeCase(t *testing.T) {
	tests := map[string]string{
		"Name":       "name",
		"MaxHP":      "max_hp",
		"HTTPServer": "http_server",
		"UserID":     "user_id",
		"X":          "x",
		"Field2":     "field2",
		"ABTest":     "ab_test",
		"already_ok": "already_ok",
		"Vec2D":      "vec2_d",
		"ÜberCool":   "über_cool",
	}
	for in, want := range tests {
		if got := SnakeCase(in); got != want {
			t.Errorf("SnakeCase(%q) = %q, want %q", in, got, want)
		}
	}
}

type Inner struct {
	A int
	B int `ron:"bee"`
}

type conflict1 struct{ X int }
type conflict2 struct{ X int }
type hidden struct{ H int }

type outer struct {
	_ struct{} `ron:"Outer,tuple"`
	Inner
	conflict1
	conflict2
	*hidden // pointer to an unexported struct cannot be allocated, so it is skipped
	A       string // shadows Inner.A
	Skip    int    `ron:"-"`
	private int
	Opt     int `ron:"opt,omitempty,omitzero"`
}

func TestFor(t *testing.T) {
	si := For(reflect.TypeFor[outer]())
	if si.Name != "Outer" || !si.Tuple {
		t.Errorf("name/tuple = %q/%v", si.Name, si.Tuple)
	}
	var names []string
	for _, f := range si.Fields {
		names = append(names, f.Name)
	}
	// X is ambiguous between two embedded structs at the same depth and is
	// dropped, like encoding/json does.
	want := []string{"bee", "a", "opt"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("fields = %v, want %v", names, want)
	}
	if f, ok := si.Lookup("OPT"); !ok || !f.OmitEmpty || !f.OmitZero {
		t.Errorf("Lookup(OPT) = %+v, %v", f, ok)
	}
	if _, ok := si.Lookup("x"); ok {
		t.Error("ambiguous field x should not be found")
	}
	if For(reflect.TypeFor[outer]()) != si {
		t.Error("Info is not cached")
	}
}

func TestByIndex(t *testing.T) {
	type E struct{ V int }
	type S struct{ *E }
	var s S
	rv := reflect.ValueOf(&s).Elem()
	if f := ByIndex(rv, []int{0, 0}, false); f.IsValid() {
		t.Error("expected invalid value for nil embedded pointer")
	}
	f := ByIndex(rv, []int{0, 0}, true)
	f.SetInt(4)
	if s.E == nil || s.V != 4 {
		t.Errorf("alloc failed: %+v", s)
	}
}
