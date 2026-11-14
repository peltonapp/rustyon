// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"errors"
	"io"
	"math"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestUnmarshalScalars(t *testing.T) {
	var (
		b   bool
		i   int
		i8  int8
		u16 uint16
		f32 float32
		f64 float64
		s   string
		r   rune
		bs  []byte
		bi  big.Int
		pbi *big.Int
	)
	tests := []struct {
		src  string
		ptr  any
		want any
	}{
		{"true", &b, true},
		{"-12", &i, -12},
		{"0x7f", &i8, int8(127)},
		{"65535", &u16, uint16(65535)},
		{"1.5", &f32, float32(1.5)},
		{"3", &f64, 3.0},
		{"inf", &f64, math.Inf(1)},
		{"inf", &f32, float32(math.Inf(1))},
		{`"text"`, &s, "text"},
		{"'c'", &s, "c"},
		{"Ident", &s, "Ident"},
		{"'x'", &r, 'x'},
		{`b"\x01\x02"`, &bs, []byte{1, 2}},
		{`"str"`, &bs, []byte("str")},
		{"[1, 2]", &bs, []byte{1, 2}},
		{"123456789012345678901234567890", &bi, *mustBig("123456789012345678901234567890").Big()},
		{"-5", &pbi, big.NewInt(-5)},
	}
	for _, tt := range tests {
		if err := Unmarshal([]byte(tt.src), tt.ptr); err != nil {
			t.Errorf("Unmarshal(%q): %v", tt.src, err)
			continue
		}
		got := reflect.ValueOf(tt.ptr).Elem().Interface()
		if g, ok := got.(big.Int); ok {
			w := tt.want.(big.Int)
			if g.Cmp(&w) != 0 {
				t.Errorf("Unmarshal(%q) = %v, want %v", tt.src, &g, &w)
			}
			continue
		}
		if g, ok := got.(*big.Int); ok {
			if g.Cmp(tt.want.(*big.Int)) != 0 {
				t.Errorf("Unmarshal(%q) = %v, want %v", tt.src, g, tt.want)
			}
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Unmarshal(%q) = %#v, want %#v", tt.src, got, tt.want)
		}
	}
}

func TestUnmarshalStruct(t *testing.T) {
	src := `
// A struct as Rust would write it.
Config(
    name: "x",
    max_hp: 10,
    ratio: 1,
    enabled: true,
    unknown_field: [1, 2, 3],
)`
	var c basicStruct
	if err := Unmarshal([]byte(src), &c); err != nil {
		t.Fatal(err)
	}
	want := basicStruct{Name: "x", MaxHP: 10, Ratio: 1, Enabled: true}
	if c != want {
		t.Errorf("got %+v, want %+v", c, want)
	}

	// Case-insensitive fallback.
	var c2 basicStruct
	if err := Unmarshal([]byte(`(NAME: "y", Max_HP: 3)`), &c2); err != nil {
		t.Fatal(err)
	}
	if c2.Name != "y" || c2.MaxHP != 3 {
		t.Errorf("case-insensitive match failed: %+v", c2)
	}

	// From a tuple, by position.
	var c3 basicStruct
	if err := Unmarshal([]byte(`("z", 1, 2.0, false)`), &c3); err != nil {
		t.Fatal(err)
	}
	if c3 != (basicStruct{"z", 1, 2, false}) {
		t.Errorf("tuple decode: %+v", c3)
	}

	// From a map with string keys.
	var c4 basicStruct
	if err := Unmarshal([]byte(`{"name": "m", "max_hp": 4}`), &c4); err != nil {
		t.Fatal(err)
	}
	if c4.Name != "m" || c4.MaxHP != 4 {
		t.Errorf("map decode: %+v", c4)
	}
}

func TestUnmarshalComposite(t *testing.T) {
	type target struct {
		Tags     []string
		Arr      [2]int
		M        map[string]int
		IntKeys  map[int]string
		Tuple    tupleStruct
		Newtype  newtype
		Embedded withEmbedded
		EmbPtr   withEmbeddedPtr
		Opt      *int
		OptNone  *int
		OptBare  *string
		OptNest  **int
		Any      any
		Value    Value
		Struct   Struct
		Text     textType
		Custom   ronType
		Time     time.Time
	}
	src := `(
		tags: ["a", "b"],
		arr: (1, 2),
		m: {"x": 1},
		int_keys: {1: "one", 2: "two"},
		tuple: (3, 4),
		newtype: 9.5,
		embedded: (base: 1, own: 2, shade: "s"),
		emb_ptr: (base: 5, own: 6),
		opt: Some(1),
		opt_none: None,
		opt_bare: "implicit",
		opt_nest: Some(Some(3)),
		any: [1, "two", 3.0, (a: true), {1: 'c'}, None, Variant],
		value: Point(x: 1),
		struct: (k: ()),
		text: "text:hello",
		custom: Custom(42),
		time: "2026-01-02T03:04:05Z",
	)`
	var got target
	if err := Unmarshal([]byte(src), &got); err != nil {
		t.Fatal(err)
	}
	three := 3
	p3 := &three
	one := 1
	implicit := "implicit"
	want := target{
		Tags:     []string{"a", "b"},
		Arr:      [2]int{1, 2},
		M:        map[string]int{"x": 1},
		IntKeys:  map[int]string{1: "one", 2: "two"},
		Tuple:    tupleStruct{X: 3, Y: 4},
		Newtype:  newtype{Inner: 9.5},
		Embedded: withEmbedded{Embedded: Embedded{Base: 1}, Own: 2, Shade: "s"},
		EmbPtr:   withEmbeddedPtr{Embedded: &Embedded{Base: 5}, Own: 6},
		Opt:      &one,
		OptBare:  &implicit,
		OptNest:  &p3,
		Any:      []any{int64(1), "two", 3.0, map[string]any{"a": true}, map[any]any{int64(1): 'c'}, nil, "Variant"},
		Value:    Struct{Name: "Point", Fields: []Field{{"x", NewInt(1)}}},
		Struct:   Struct{Fields: []Field{{"k", Unit{}}}},
		Text:     textType{"hello"},
		Custom:   ronType{42},
		Time:     time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %#v\nwant %#v", got, want)
	}
}

func TestUnmarshalAnyNumbers(t *testing.T) {
	var v any
	if err := Unmarshal([]byte("[1, 18446744073709551615, 100000000000000000000000]"), &v); err != nil {
		t.Fatal(err)
	}
	l := v.([]any)
	if l[0] != int64(1) || l[1] != uint64(math.MaxUint64) {
		t.Errorf("got %#v", l)
	}
	if b, ok := l[2].(*big.Int); !ok || b.String() != "100000000000000000000000" {
		t.Errorf("big: %#v", l[2])
	}
}

func TestUnmarshalErrors(t *testing.T) {
	tests := []struct {
		src  string
		ptr  any
		path string
		msg  string
	}{
		{`"x"`, new(int), "", "cannot unmarshal string into Go value of type int"},
		{"300", new(uint8), "", "overflows"},
		{"-1", new(uint), "", "overflows"},
		{"99999999999999999999", new(int64), "", "overflows"},
		{"1e300", new(float32), "", "float overflows"},
		{"(a: 1)", new([]int), "", "cannot unmarshal struct"},
		{"[1, 2, 3]", new([2]int), "", "expected 2 elements, found 3"},
		{`(name: 5)`, new(basicStruct), "name", "cannot unmarshal integer"},
		{`(tags: [1])`, new(struct{ Tags []string }), "tags[0]", "integer"},
		{`{"a": "b"}`, new(map[string]int), `["a"]`, "string"},
		{`{[1]: 1}`, new(any), "", "not comparable"},
		{"(1, 2)", new(tupleStruct), "", ""},
		{"(1, 2, 3)", new(tupleStruct), "", "expected 2 elements"},
		{"5", new(chan int), "", "unsupported Go type"},
		{"5", new(error), "", "not a registered enum"},
		{`{1: 2}`, new(basicStruct), "", "map keys must be strings"},
		{`5`, new(textType), "", "expected a string"},
	}
	for _, tt := range tests {
		err := Unmarshal([]byte(tt.src), tt.ptr)
		if tt.msg == "" {
			if err != nil {
				t.Errorf("Unmarshal(%q): unexpected error %v", tt.src, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("Unmarshal(%q, %T): expected error containing %q", tt.src, tt.ptr, tt.msg)
			continue
		}
		if !strings.Contains(err.Error(), tt.msg) {
			t.Errorf("Unmarshal(%q): error %q does not contain %q", tt.src, err, tt.msg)
		}
		var te *UnmarshalTypeError
		if errors.As(err, &te) && te.Path != tt.path {
			t.Errorf("Unmarshal(%q): path %q, want %q", tt.src, te.Path, tt.path)
		}
	}
}

func TestInvalidUnmarshal(t *testing.T) {
	var x int
	for _, v := range []any{nil, x, (*int)(nil)} {
		err := Unmarshal([]byte("1"), v)
		var ie *InvalidUnmarshalError
		if !errors.As(err, &ie) {
			t.Errorf("Unmarshal(%T): got %v", v, err)
		}
		if ie.Error() == "" {
			t.Error("empty error message")
		}
	}
	if err := Unmarshal([]byte("[1"), &x); err == nil {
		t.Error("expected syntax error")
	}
}

func TestDecoderStream(t *testing.T) {
	src := "#![enable(implicit_some)]\n1 // one\n(a: 2)\n/* three */ [3]\n"
	dec := NewDecoder(strings.NewReader(src))
	var got []any
	for dec.More() {
		var v any
		if err := dec.Decode(&v); err != nil {
			t.Fatal(err)
		}
		got = append(got, v)
	}
	want := []any{int64(1), map[string]any{"a": int64(2)}, []any{int64(3)}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
	var v any
	if err := dec.Decode(&v); !errors.Is(err, io.EOF) {
		t.Errorf("after end: %v, want io.EOF", err)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestDecoderErrors(t *testing.T) {
	dec := NewDecoder(errReader{})
	var v any
	if err := dec.Decode(&v); err == nil || err.Error() != "read failed" {
		t.Errorf("got %v", err)
	}
	if !dec.More() {
		t.Error("More should report true so Decode surfaces the error")
	}

	dec = NewDecoder(strings.NewReader("(name: \"a\", extra: 1)"))
	dec.DisallowUnknownFields()
	var c basicStruct
	err := dec.Decode(&c)
	var ue *UnknownFieldError
	if !errors.As(err, &ue) || ue.Field != "extra" {
		t.Errorf("got %v, want UnknownFieldError for extra", err)
	}

	dec = NewDecoder(strings.NewReader("[1, oops"))
	if err := dec.Decode(&v); err == nil {
		t.Error("expected syntax error")
	}
}

func TestFromValue(t *testing.T) {
	v := Struct{Fields: []Field{{"name", String("v")}, {"max_hp", NewInt(1)}}}
	var c basicStruct
	if err := FromValue(v, &c); err != nil {
		t.Fatal(err)
	}
	if c.Name != "v" || c.MaxHP != 1 {
		t.Errorf("got %+v", c)
	}
}

func TestRoundTrip(t *testing.T) {
	type nested struct {
		A []map[string][]tupleStruct
		B *nested
		C [3]float32
		D map[int8]*string
		E []byte
		F any
	}
	s := "str"
	in := nested{
		A: []map[string][]tupleStruct{{"k": {{X: 1, Y: 2}}}},
		B: &nested{C: [3]float32{0.1, 0.2, 0.3}},
		C: [3]float32{1, 2, 3},
		D: map[int8]*string{-1: &s, 2: nil},
		E: []byte{0, 1, 255},
		F: "anything",
	}
	for _, indent := range []string{"", "\t"} {
		var data []byte
		var err error
		if indent == "" {
			data, err = Marshal(in)
		} else {
			data, err = MarshalIndent(in, "", indent)
		}
		if err != nil {
			t.Fatal(err)
		}
		var out nested
		if err := Unmarshal(data, &out); err != nil {
			t.Fatalf("%v\n%s", err, data)
		}
		// Nil and empty slices or maps encode identically, so compare the
		// re-encoded output instead of the Go values.
		again, err := Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		first, _ := Marshal(in)
		if string(first) != string(again) {
			t.Errorf("round trip mismatch\nfirst  %s\nsecond %s", first, again)
		}
	}
}
