// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"bytes"
	"errors"
	"math"
	"math/big"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type basicStruct struct {
	Name    string
	MaxHP   int
	Ratio   float64
	Enabled bool
}

type taggedStruct struct {
	A       int    `ron:"alpha"`
	B       string `ron:",omitempty"`
	C       *int   `ron:",omitempty"`
	D       int    `ron:"-"`
	E       []int  `ron:"e,omitempty"`
	F       Zeroer `ron:",omitzero"`
	private int    //nolint:unused // checks that unexported fields are skipped
}

type Zeroer struct{ V int }

func (z Zeroer) IsZero() bool { return z.V <= 0 }

type tupleStruct struct {
	_    struct{} `ron:",tuple"`
	X, Y int
}

type namedStruct struct {
	_ struct{} `ron:"Renamed"`
	V int
}

type newtype struct {
	_     struct{} `ron:",tuple"`
	Inner float64
}

type Embedded struct {
	Base  int
	Shade string
}

type withEmbedded struct {
	Embedded
	Own   int
	Shade string // shadows Embedded.Shade
}

type withEmbeddedPtr struct {
	*Embedded
	Own int
}

type unitStruct struct{}

type textType struct{ s string }

func (t textType) MarshalText() ([]byte, error) { return []byte("text:" + t.s), nil }
func (t *textType) UnmarshalText(b []byte) error {
	t.s = strings.TrimPrefix(string(b), "text:")
	return nil
}

type ronType struct{ n int }

func (r ronType) MarshalRON() ([]byte, error) {
	return Marshal(Tuple{Name: "Custom", Values: []Value{NewInt(int64(r.n))}})
}

func (r *ronType) UnmarshalRON(b []byte) error {
	v, err := Parse(b)
	if err != nil {
		return err
	}
	t, ok := v.(Tuple)
	if !ok || t.Name != "Custom" || len(t.Values) != 1 {
		return errors.New("bad custom value")
	}
	n, _ := t.Values[0].(Int).Int64()
	r.n = int(n)
	return nil
}

type failingMarshaler struct{}

func (failingMarshaler) MarshalRON() ([]byte, error) { return nil, errors.New("boom") }

func TestMarshal(t *testing.T) {
	seven := 7
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, "None"},
		{"bool", true, "true"},
		{"int", -5, "-5"},
		{"uint64 max", uint64(math.MaxUint64), "18446744073709551615"},
		{"float", 1.5, "1.5"},
		{"integral float", 2.0, "2.0"},
		{"float32", float32(0.1), "0.1"},
		{"exp float", 1e21, "1e+21"},
		{"nan", math.NaN(), "NaN"},
		{"inf", math.Inf(-1), "-inf"},
		{"string", "a\"b\\c\nd\u0001", `"a\"b\\c\nd\u{1}"`},
		{"unicode string", "héllo ☺", `"héllo ☺"`},
		{"bytes", []byte("a\x00\xff\""), `b"a\x00\xff\""`},
		{"nil bytes", []byte(nil), `b""`},
		{"slice", []int{1, 2}, "[1,2]"},
		{"nil slice", []int(nil), "[]"},
		{"array", [2]string{"a", "b"}, `["a","b"]`},
		{"map sorted", map[string]int{"b": 2, "a": 1}, `{"a":1,"b":2}`},
		{"int map sorted numerically", map[int]bool{10: true, 9: false, -1: true}, `{-1:true,9:false,10:true}`},
		{"nil ptr", (*int)(nil), "None"},
		{"ptr", &seven, "Some(7)"},
		{"struct", basicStruct{"x", 10, 0.5, true}, `(name:"x",max_hp:10,ratio:0.5,enabled:true)`},
		{"tags", taggedStruct{A: 1, D: 4}, `(alpha:1)`},
		{"tags full", taggedStruct{A: 1, B: "b", C: &seven, E: []int{1}, F: Zeroer{2}}, `(alpha:1,b:"b",c:Some(7),e:[1],f:(v:2))`},
		{"tuple struct", tupleStruct{X: 1, Y: 2}, "(1,2)"},
		{"newtype", newtype{Inner: 1.5}, "(1.5)"},
		{"embedded", withEmbedded{Embedded{1, "inner"}, 2, "outer"}, `(base:1,own:2,shade:"outer")`},
		{"nil embedded ptr", withEmbeddedPtr{Own: 3}, `(own:3)`},
		{"embedded ptr", withEmbeddedPtr{&Embedded{Base: 1}, 3}, `(base:1,shade:"",own:3)`},
		{"unit struct", unitStruct{}, "()"},
		{"anonymous struct", struct{ A, B int }{1, 2}, "(a:1,b:2)"},
		{"interface slice", []any{1, "a", nil, []any{true}}, `[1,"a",None,[true]]`},
		{"text marshaler", textType{"x"}, `"text:x"`},
		{"netip", netip.MustParseAddr("10.0.0.1"), `"10.0.0.1"`},
		{"time", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), `"2026-01-02T03:04:05Z"`},
		{"duration", 2 * time.Second, "2000000000"},
		{"ron marshaler", ronType{5}, "Custom(5)"},
		{"ron marshaler ptr", &ronType{5}, "Custom(5)"},
		{"big.Int", big.NewInt(-3), "-3"},
		{"big.Int value", *big.NewInt(9), "9"},
		{"value tree", Struct{Name: "P", Fields: []Field{{"x", Char('c')}}}, "P(x:'c')"},
		{"value in struct", struct{ V Value }{List{Unit{}}}, "(v:[()])"},
		{"raw ident field", struct {
			T int `ron:"type"`
		}{1}, "(type:1)"},
		{"reserved field name", struct {
			T int `ron:"true"`
		}{1}, "(r#true:1)"},
		{"dashed field name", struct {
			T int `ron:"a-b"`
		}{1}, "(r#a-b:1)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Marshal(tt.in)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("Marshal = %s, want %s", got, tt.want)
			}
			if !Valid(got) {
				t.Errorf("output is not valid RON: %s", got)
			}
		})
	}
}

func TestMarshalNamedStructs(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	enc.SetStructNames(true)
	for _, v := range []any{basicStruct{Name: "a"}, tupleStruct{X: 1}, namedStruct{V: 1}, unitStruct{}, struct{ A int }{1}} {
		if err := enc.Encode(v); err != nil {
			t.Fatal(err)
		}
	}
	want := `basicStruct(name:"a",max_hp:0,ratio:0.0,enabled:false)
tupleStruct(1,0)
Renamed(v:1)
unitStruct
(a:1)
`
	if buf.String() != want {
		t.Errorf("got\n%s\nwant\n%s", buf.String(), want)
	}
}

func TestMarshalIndent(t *testing.T) {
	type inner struct {
		Tags []string
		Pos  tupleStruct
	}
	v := struct {
		Name  string
		Inner inner
		Empty []int
		M     map[string]int
		Opt   *inner
	}{"n", inner{[]string{"a", "b"}, tupleStruct{X: 1, Y: 2}}, nil, map[string]int{"k": 1}, nil}
	got, err := MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want := `(
  name: "n",
  inner: (
    tags: [
      "a",
      "b",
    ],
    pos: (1, 2),
  ),
  empty: [],
  m: {
    "k": 1,
  },
  opt: None,
)`
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}

	got, err = MarshalIndent([]int{1}, "> ", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := "[\n>     1,\n> ]"; string(got) != want {
		t.Errorf("prefix: got %q, want %q", got, want)
	}
}

func TestEncoderExtensions(t *testing.T) {
	seven := 7
	v := struct {
		P *int
		N newtype
	}{&seven, newtype{Inner: 2}}

	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	enc.SetImplicitSome(true)
	enc.SetUnwrapNewtypes(true)
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	want := "#![enable(implicit_some,unwrap_newtypes)](p:7,n:2.0)\n"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}

	buf.Reset()
	enc.SetIndent("", "    ")
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	want = "#![enable(implicit_some, unwrap_newtypes)]\n(\n    p: 7,\n    n: 2.0,\n)\n"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}

	var back struct {
		P *int
		N newtype
	}
	if err := Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatal(err)
	}
	if back.P == nil || *back.P != 7 || back.N.Inner != 2 {
		t.Errorf("round trip mismatch: %+v", back)
	}
}

func TestMarshalErrors(t *testing.T) {
	type cyclic struct{ Next *cyclic }
	c := &cyclic{}
	c.Next = c

	tests := []struct {
		name string
		in   any
		want any
	}{
		{"chan", make(chan int), new(*UnsupportedTypeError)},
		{"func", func() {}, new(*UnsupportedTypeError)},
		{"complex", complex(1, 2), new(*UnsupportedTypeError)},
		{"marshaler error", failingMarshaler{}, new(*MarshalerError)},
		{"cycle", c, new(*UnsupportedValueError)},
		{"bad field name", struct {
			A int `ron:"has space"`
		}{}, new(*UnsupportedValueError)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Marshal(tt.in)
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.As(err, tt.want) {
				t.Errorf("got %T (%v)", err, err)
			}
		})
	}

	var me *MarshalerError
	_, err := Marshal(failingMarshaler{})
	if errors.As(err, &me) && errors.Unwrap(me) == nil {
		t.Error("MarshalerError does not unwrap")
	}
}

func TestToValue(t *testing.T) {
	v, err := ToValue(basicStruct{Name: "a", MaxHP: 2})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := v.(Struct)
	if !ok {
		t.Fatalf("got %T", v)
	}
	if hp, _ := s.Get("max_hp"); !Equal(hp, NewInt(2)) {
		t.Errorf("max_hp = %#v", hp)
	}
}
