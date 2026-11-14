// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"fmt"
	"testing"
)

func TestValueString(t *testing.T) {
	v, err := Parse([]byte(`(a: Some(1), b: [1, 2], c: {"k": None}, d: Pair(1, 'x'), e: (), f: Unit(x: 1))`))
	if err != nil {
		t.Fatal(err)
	}
	s := v.(Struct)
	get := func(name string) Value {
		x, _ := s.Get(name)
		return x
	}
	cases := []struct {
		got  any
		want string
	}{
		{v, `(a:Some(1),b:[1,2],c:{"k":None},d:Pair(1,'x'),e:(),f:Unit(x:1))`},
		{get("a"), `Some(1)`},
		{get("b"), `[1,2]`},
		{get("c"), `{"k":None}`},
		{get("d"), `Pair(1,'x')`},
		{get("e"), `()`},
		{Option{}, `None`},
		{s.Fields[0], `a:Some(1)`},
		{get("c").(Map)[0], `"k":None`},
		{[]Value{Some(NewInt(1)), List{}}, `[Some(1) []]`},
		// Scalars keep their default formatting.
		{String("x"), `x`},
		{Bool(true), `true`},
		{NewInt(7), `7`},
	}
	for _, c := range cases {
		if got := fmt.Sprint(c.got); got != c.want {
			t.Errorf("Sprint = %q, want %q", got, c.want)
		}
	}
	if got := fmt.Sprintf("%v", Some(NewInt(1))); got != "Some(1)" {
		t.Errorf("%v: %q", Some(NewInt(1)), got)
	}
}
