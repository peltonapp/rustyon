// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/peltonapp/rustyon-go/internal/fields"
)

type namingCfg struct {
	WindowSize [2]int
	MaxHP      int
	Explicit   int `ron:"Custom_Name"`
	Nested     namingInner
	Fixed      namingFixed
}

type namingInner struct{ InnerField int }

type namingFixed struct {
	_     struct{} `ron:",rename_all=PascalCase"`
	SomeX int
}

func TestFieldNamingEncodeDecode(t *testing.T) {
	v := namingCfg{WindowSize: [2]int{1, 2}, MaxHP: 3, Explicit: 4, Nested: namingInner{5}, Fixed: namingFixed{SomeX: 6}}
	cases := []struct {
		n    NamingStrategy
		want string
	}{
		{SnakeCase, `(window_size:[1,2],max_hp:3,Custom_Name:4,nested:(inner_field:5),fixed:(SomeX:6))`},
		{CamelCase, `(windowSize:[1,2],maxHp:3,Custom_Name:4,nested:(innerField:5),fixed:(SomeX:6))`},
		{PascalCase, `(WindowSize:[1,2],MaxHp:3,Custom_Name:4,Nested:(InnerField:5),Fixed:(SomeX:6))`},
		{ScreamingSnakeCase, `(WINDOW_SIZE:[1,2],MAX_HP:3,Custom_Name:4,NESTED:(INNER_FIELD:5),FIXED:(SomeX:6))`},
		{KebabCase, `(r#window-size:[1,2],r#max-hp:3,Custom_Name:4,nested:(r#inner-field:5),fixed:(SomeX:6))`},
		{Identity, `(WindowSize:[1,2],MaxHP:3,Custom_Name:4,Nested:(InnerField:5),Fixed:(SomeX:6))`},
		{LowerCase, `(windowsize:[1,2],maxhp:3,Custom_Name:4,nested:(innerfield:5),fixed:(SomeX:6))`},
	}
	for _, c := range cases {
		out, err := MarshalWith(v, EncodeOptions{FieldNaming: c.n})
		if err != nil {
			t.Fatal(c.n, err)
		}
		if string(out) != c.want {
			t.Errorf("%v: got %s\nwant %s", c.n, out, c.want)
		}
		var back namingCfg
		if err := UnmarshalWith(out, &back, DecodeOptions{FieldNaming: c.n, DisallowUnknownFields: true}); err != nil {
			t.Fatalf("%v: %v", c.n, err)
		}
		if !reflect.DeepEqual(back, v) {
			t.Errorf("%v: round trip %+v", c.n, back)
		}
		// The tree decoder must agree.
		var viaTree namingCfg
		if err := unmarshalTree(out, &viaTree, &decodeOptions{naming: fields.Naming(c.n), disallowUnknown: true}); err != nil || !reflect.DeepEqual(viaTree, v) {
			t.Errorf("%v: tree decode %+v, %v", c.n, viaTree, err)
		}
	}
	// Default behavior is unchanged.
	def, _ := Marshal(v)
	if string(def) != cases[0].want {
		t.Errorf("Marshal changed: %s", def)
	}
}

func TestFieldNamingEncoderDecoder(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	enc.SetFieldNaming(CamelCase)
	if err := enc.Encode(namingInner{7}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "(innerField:7)\n" {
		t.Fatal(buf.String())
	}
	dec := NewDecoder(&buf)
	dec.SetFieldNaming(CamelCase)
	dec.DisallowUnknownFields()
	var in namingInner
	if err := dec.Decode(&in); err != nil || in.InnerField != 7 {
		t.Fatal(in, err)
	}
	// With the default naming the camelCase field is unknown.
	var other namingInner
	if err := Unmarshal([]byte(`(innerField:7)`), &other); err != nil || other.InnerField != 0 {
		t.Fatal(other, err)
	}
}

type namingEnumShape interface{ isNamingShape() }

type namingCircle struct{ BigRadius float64 }

func (namingCircle) isNamingShape() {}

func TestFieldNamingVariants(t *testing.T) {
	r := NewRegistry()
	RegisterEnumIn[namingEnumShape](r, Variant[namingCircle]("Circle"))
	var s namingEnumShape = namingCircle{2}
	out, err := MarshalWith(s, EncodeOptions{FieldNaming: CamelCase, Registry: r})
	if err != nil || string(out) != `Circle(bigRadius:2.0)` {
		t.Fatalf("%s %v", out, err)
	}
	var back namingEnumShape
	if err := UnmarshalWith(out, &back, DecodeOptions{FieldNaming: CamelCase, Registry: r}); err != nil || back != s {
		t.Fatalf("%v %v", back, err)
	}
}

func TestNamingStrategyHelpers(t *testing.T) {
	if CamelCase.Name("MaxHP") != "maxHp" || CamelCase.String() != "camelCase" {
		t.Fatal(CamelCase.Name("MaxHP"), CamelCase)
	}
	if !strings.Contains(KebabCase.Name("MaxHP"), "-") {
		t.Fatal("kebab")
	}
}
