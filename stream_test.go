// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// The tests in this file compare the streaming decoder with the tree based
// decoder (Parse followed by FromValue semantics). Both must accept exactly
// the same inputs and produce the same Go values. Error values are produced
// by the tree decoder in all cases, so only success and the result are
// compared.

type sInner struct {
	X int32
	Y string `ron:"why"`
}

type sNewtype struct {
	_ struct{} `ron:"Newtype,tuple"`
	V float64
}

type sPair struct {
	_ struct{} `ron:",tuple"`
	A uint8
	B string
}

type SEmbedded struct {
	E1 int
	E2 []string
}

type sShape interface{ isShape() }

type sCircle struct{ Radius float64 }

type sRect struct {
	_ struct{} `ron:",tuple"`
	W uint8
	H uint8
}

type sDot struct{}

type sLabel string

func (sCircle) isShape() {}
func (sRect) isShape()   {}
func (sDot) isShape()    {}
func (sLabel) isShape()  {}

type sColor int

type sText struct{ S string }

func (t *sText) UnmarshalText(b []byte) error {
	if string(b) == "bad" {
		return fmt.Errorf("bad text")
	}
	t.S = string(b)
	return nil
}

type sRaw struct{ Raw string }

func (r *sRaw) UnmarshalRON(b []byte) error {
	if bytes.Contains(b, []byte("bad")) {
		return fmt.Errorf("bad raw")
	}
	r.Raw = string(b)
	return nil
}

type sTarget struct {
	_      struct{} `ron:"Target"`
	B      bool
	I      int
	I8     int8
	U      uint
	U16    uint16
	F32    float32
	F64    float64
	S      string
	By     []byte
	L      []int
	Arr    [2]string
	M      map[string]int
	MI     map[int]string
	MA     map[string]any
	P      *int
	PP     **string
	In     sInner
	PIn    *sInner
	Nt     sNewtype
	Pair   sPair
	Opt    *[]int
	Any    any
	Val    Value
	Sh     sShape
	Shapes []sShape
	Col    sColor
	Big    *big.Int
	Txt    sText
	Raw    sRaw
	Rune   rune
	Tup    [2]float64
	Nested []map[string][]sInner
	SEmbedded
}

var streamRegistry = func() *Registry {
	r := NewRegistry()
	RegisterEnumIn[sShape](r,
		Variant[sCircle]("Circle"),
		Variant[sRect]("Rect"),
		Variant[sDot]("Dot"),
		Variant[sLabel]("Label"),
	)
	RegisterUnitEnumIn(r, map[sColor]string{0: "Red", 1: "Green", 2: "Blue"})
	return r
}()

var streamTargets = []struct {
	name string
	mk   func() any
}{
	{"target", func() any { return new(sTarget) }},
	{"targets", func() any { return new([]sTarget) }},
	{"ints", func() any { return new([]int) }},
	{"bytes", func() any { return new([]byte) }},
	{"intarray", func() any { return new([3]int) }},
	{"strmap", func() any { return new(map[string]int) }},
	{"floatmap", func() any { return new(map[float64]string) }},
	{"anymap", func() any { return new(map[any]any) }},
	{"strings", func() any { return new([]string) }},
	{"ptr", func() any { return new(*int) }},
	{"ptrptr", func() any { return new(**sInner) }},
	{"string", func() any { return new(string) }},
	{"f32", func() any { return new(float32) }},
	{"f64", func() any { return new(float64) }},
	{"u8", func() any { return new(uint8) }},
	{"i64", func() any { return new(int64) }},
	{"u64", func() any { return new(uint64) }},
	{"bool", func() any { return new(bool) }},
	{"rune", func() any { return new(rune) }},
	{"inner", func() any { return new(sInner) }},
	{"newtype", func() any { return new(sNewtype) }},
	{"pair", func() any { return new(sPair) }},
	{"pairs", func() any { return new([]sPair) }},
	{"any", func() any { return new(any) }},
	{"value", func() any { return new(Value) }},
	{"shape", func() any { return new(sShape) }},
	{"shapes", func() any { return new([]sShape) }},
	{"color", func() any { return new(sColor) }},
	{"big", func() any { return new(*big.Int) }},
	{"text", func() any { return new(sText) }},
	{"raw", func() any { return new(sRaw) }},
	{"empty", func() any { return new(struct{}) }},
	{"opts", func() any { return new([]*[]*string) }},
	{"embedded", func() any { return new(SEmbedded) }},
	{"unit", func() any { return new(struct{ A struct{} }) }},
}

func sameGo(a, b reflect.Value) bool {
	if a.IsValid() != b.IsValid() {
		return false
	}
	if !a.IsValid() {
		return true
	}
	if a.Type() != b.Type() {
		return false
	}
	if a.CanInterface() {
		switch x := a.Interface().(type) {
		case Value:
			return Equal(x, b.Interface().(Value))
		case big.Int:
			y := b.Interface().(big.Int)
			return x.Cmp(&y) == 0
		case *big.Int:
			y := b.Interface().(*big.Int)
			if x == nil || y == nil {
				return x == nil && y == nil
			}
			return x.Cmp(y) == 0
		}
	}
	switch a.Kind() {
	case reflect.Pointer, reflect.Interface:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() == b.IsNil()
		}
		return sameGo(a.Elem(), b.Elem())
	case reflect.Slice:
		if a.IsNil() != b.IsNil() || a.Len() != b.Len() {
			return false
		}
		fallthrough
	case reflect.Array:
		for i := range a.Len() {
			if !sameGo(a.Index(i), b.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Map:
		if a.IsNil() != b.IsNil() || a.Len() != b.Len() {
			return false
		}
		for _, k := range a.MapKeys() {
			// NaN keys never match themselves; the lengths were compared.
			if k.Kind() == reflect.Interface && !k.IsNil() && k.Elem().Kind() == reflect.Float64 && math.IsNaN(k.Elem().Float()) {
				continue
			}
			bv := b.MapIndex(k)
			if !bv.IsValid() || !sameGo(a.MapIndex(k), bv) {
				return false
			}
		}
		return true
	case reflect.Struct:
		for i := range a.NumField() {
			if a.Type().Field(i).Name == "_" {
				continue
			}
			if !sameGo(a.Field(i), b.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Float32, reflect.Float64:
		x, y := a.Float(), b.Float()
		if math.IsNaN(x) || math.IsNaN(y) {
			return math.IsNaN(x) && math.IsNaN(y)
		}
		return x == y
	}
	return reflect.DeepEqual(a.Interface(), b.Interface())
}

// checkStream decodes data with both decoders into a fresh value of every
// target type and reports differences.
func checkStream(t testing.TB, data []byte, opts decodeOptions) {
	t.Helper()
	for _, tg := range streamTargets {
		a, b := tg.mk(), tg.mk()
		oa, ob := opts, opts
		errA := unmarshalStream(data, a, &oa)
		errB := unmarshalTree(data, b, &ob)
		if (errA == nil) != (errB == nil) {
			t.Fatalf("target %s: streaming err=%v, tree err=%v\ninput: %q\nopts: %+v", tg.name, errA, errB, data, opts)
		}
		if errA != nil {
			continue
		}
		if !sameGo(reflect.ValueOf(a), reflect.ValueOf(b)) {
			t.Fatalf("target %s: results differ\ninput: %q\nstream: %#v\ntree:   %#v\nopts: %+v", tg.name, data, reflect.ValueOf(a).Elem().Interface(), reflect.ValueOf(b).Elem().Interface(), opts)
		}
	}
}

var streamOptionSets = []decodeOptions{
	{},
	{disallowUnknown: true},
	{registry: streamRegistry},
	{registry: streamRegistry, disallowUnknown: true},
}

var streamSeeds = []string{
	`(b: true, i: -5, i8: 127, u: 7, u16: 65535, f32: 1.5, f64: -2e3, s: "hi\n", by: b"\x01\x02", l: [1, 2, 3], arr: ["a", "b"], m: {"a": 1, "b": 2}, mi: {1: "x"}, ma: {"k": [1, 'c']}, p: 3, pp: "s", in: (x: 4, why: "w"), pin: (x: 1), nt: Newtype(2.5), pair: (3, "p"), opt: Some([1]), any: [1, 2], val: Foo(1), sh: Circle(radius: 1.0), shapes: [Rect(1, 2), Dot, Label("x"), Circle((radius: 2.0))], col: Green, big: 123456789012345678901234567890, txt: "text", raw: (a: 1), rune: 'x', tup: (1.0, 2), nested: [{"a": [(x: 1)]}], e1: 9, e2: ["z"])`,
	`Target(b: false, L: [], Arr: ("a", "b"), Unknown: [1, (2)], Nt: 5.0, Pair: Pair(1, "x"))`,
	`#![enable(implicit_some, unwrap_newtypes, unwrap_variant_newtypes)]
Target(p: 3, opt: [1], nt: 5.0, pair: (1, "a"), sh: Circle(radius: 3.0), shapes: [Rect(1, 2), Label("q")], pin: (x: 1, why: "a"))`,
	`#![enable(explicit_struct_names)]
Target(in: Inner(x: 1), nt: Newtype(1.0), pair: Pair(1, "b"))`,
	`#![enable(unwrap_variant_newtypes)] Some(Some(a: 1))`,
	`#![enable(unwrap_variant_newtypes)] Some(Foo(1, 2))`,
	`#![enable(implicit_some)] Some 5`,
	`Some(5)`, `None`, `Some(None)`, `()`, `( )`, `[]`, `[1, 2, 3]`, `[1, 2, 3,]`, `(1, 2)`, `Foo(1)`, `Foo(1, 2)`, `Meters(5.0)`,
	`{"a": 1}`, `{1: 2, 3: 4}`, `{}`, `(a: 1)`, `Foo(a: 1)`, `(x: 1, why: "q")`, `Inner(X: 1, WHY: "z")`, `(1, "a")`, `Newtype(1.5)`,
	`1..5`, `1..=5`, `..5`, `..`, `1.0..2.0`, `inf..`, `(start: 1, end: 2)`, `{"start": 1, "end": 2}`,
	`1`, `-1`, `+1`, `0x_ff`, `1_000`, `255u8`, `-129i8`, `1e3`, `1.5f32`, `1.`, `.5`, `-0`, `18446744073709551615`, `-9223372036854775808`, `170141183460469231731687303715884105728`,
	`inf`, `-inf`, `NaN`, `true`, `false`, `'a'`, `'\u{1F600}'`, `"s"`, `"a\u{e9}b"`, `r#"raw"#`, `b"bytes"`, `br"raw"`, `b'a'`,
	`Red`, `Green`, `"Blue"`, `Circle(radius: 1.0)`, `Circle((radius: 1.0))`, `Rect(1, 2)`, `Dot`, `Label("a")`, `Label(("a"))`, `[Circle(radius: 1.0), Dot]`,
	`"text"`, `bad`, `(raw: 1)`, `123456789012345678901234567890`, `-123456789012345678901234567890`,
	`[[1, 2], [3]]`, `[(a: 1), (b: 2)]`, `[Some(1), None]`, `[[[[[[[[1]]]]]]]]`,
	`(a: 1,)`, `(a: 1 b: 2)`, `[1 2]`, `[1,,2]`, `{"a" 1}`, `(1,)`, `(1)`, `Foo()`, `Foo( )`, `a`, `r#a`, `r#a(b: 1)`,
	`[1, 2] trailing`, `// c
 [1] /* d */`, `/* unclosed`, `"unterminated`, `'ab'`, `(x: 1, x: 2)`, `{"a": 1, "a": 2}`,
}

func TestStreamAgainstTree(t *testing.T) {
	seeds := append([]string(nil), streamSeeds...)
	for _, pat := range []string{"testdata/compat/*", "testdata/format/*.ron"} {
		files, _ := filepath.Glob(pat)
		for _, name := range files {
			b, err := os.ReadFile(name)
			if err != nil {
				continue
			}
			if strings.HasSuffix(name, ".txt") {
				seeds = append(seeds, strings.Split(string(b), "\n")...)
			} else {
				seeds = append(seeds, string(b))
			}
		}
	}
	for _, s := range seeds {
		for _, opts := range streamOptionSets {
			checkStream(t, []byte(s), opts)
		}
	}
}

var streamExtensionPrefixes = []string{
	"",
	"#![enable(implicit_some)]\n",
	"#![enable(unwrap_newtypes)]\n",
	"#![enable(unwrap_variant_newtypes)]\n",
	"#![enable(explicit_struct_names)]\n",
	"#![enable(implicit_some, unwrap_newtypes, unwrap_variant_newtypes)]\n",
}

// FuzzStream checks that the streaming decoder and the tree decoder agree on
// every input, for several target types, extension sets and options.
func FuzzStream(f *testing.F) {
	for i, s := range streamSeeds {
		f.Add([]byte(s), uint8(i), uint8(i/3))
	}
	f.Fuzz(func(t *testing.T, data []byte, exts uint8, opt uint8) {
		prefix := streamExtensionPrefixes[int(exts)%len(streamExtensionPrefixes)]
		checkStream(t, []byte(prefix+string(data)), streamOptionSets[int(opt)%len(streamOptionSets)])
	})
}

var hookCalls int

type countingHook struct{ N int }

func (c *countingHook) UnmarshalRON([]byte) error {
	hookCalls++
	c.N = hookCalls
	return nil
}

type countingText struct{ S string }

func (c *countingText) UnmarshalText(b []byte) error {
	hookCalls++
	c.S = string(b)
	return nil
}

type failingHook struct{}

func (failingHook) UnmarshalRON([]byte) error { return fmt.Errorf("hook failed") }

// TestStreamHooksRunOnce checks that a failing decode runs each hook at most
// once, whether the failure is a later syntax error, a later type error or
// an error of the hook itself.
func TestStreamHooksRunOnce(t *testing.T) {
	type doc struct {
		H countingHook
		T countingText
		N int
	}
	cases := []struct {
		name, in string
		want     any
	}{
		{"syntax", `(h: 1, t: "x", n: 2,, )`, new(SyntaxError)},
		{"type", `(h: 1, t: "x", n: "no")`, new(UnmarshalTypeError)},
	}
	for _, c := range cases {
		hookCalls = 0
		var d doc
		err := Unmarshal([]byte(c.in), &d)
		if err == nil {
			t.Fatalf("%s: want error", c.name)
		}
		switch want := c.want.(type) {
		case *SyntaxError:
			if !errors.As(err, &want) {
				t.Fatalf("%s: want syntax error, got %v", c.name, err)
			}
		case *UnmarshalTypeError:
			if !errors.As(err, &want) {
				t.Fatalf("%s: want type error, got %v", c.name, err)
			}
		}
		// One call each for the RON hook and the text hook.
		if hookCalls != 2 {
			t.Fatalf("%s: hooks ran %d times, want 2", c.name, hookCalls)
		}
	}

	var f struct {
		A failingHook
		B int
	}
	err := Unmarshal([]byte(`(a: 1, b: "no")`), &f)
	if err == nil || err.Error() != "hook failed" {
		t.Fatalf("want the hook error, got %v", err)
	}
}

// TestStreamConcurrent decodes into types whose plans are built lazily from
// many goroutines, for the race detector.
func TestStreamConcurrent(t *testing.T) {
	data := []byte(`(b: true, l: [1, 2], in: (x: 1), nested: [{"a": [(x: 2)]}])`)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var v struct {
				B      bool
				L      []int
				In     sInner
				Nested []map[string][]sInner
			}
			if err := Unmarshal(data, &v); err != nil || !v.B || len(v.L) != 2 || v.Nested[0]["a"][0].X != 2 {
				t.Error(v, err)
			}
		}()
	}
	wg.Wait()
}

// TestStreamAliasing checks that decoded strings do not keep the input alive
// by pointing into it and that decoding into a populated slice starts fresh.
func TestStreamAliasing(t *testing.T) {
	data := []byte(`["abc", "d\ne"]`)
	var s []string
	if err := Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	copy(data, strings.Repeat("x", len(data)))
	if s[0] != "abc" || s[1] != "d\ne" {
		t.Fatalf("strings changed with the input: %q", s)
	}
	type item struct{ A, B int }
	old := []item{{1, 2}, {3, 4}}
	if err := Unmarshal([]byte(`[(a: 9)]`), &old); err != nil {
		t.Fatal(err)
	}
	if len(old) != 1 || old[0] != (item{9, 0}) {
		t.Fatalf("got %v", old)
	}
}

// TestStreamDecoderSequence checks that a Decoder over several values keeps
// working after type errors and stops at syntax errors.
func TestStreamDecoderSequence(t *testing.T) {
	input := "#![enable(implicit_some)]\n1 [1,2] (x: 1, why: \"a\") \"str\" Some(4) [1,"
	dec := NewDecoder(strings.NewReader(input))
	var n int
	if err := dec.Decode(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	var l []int
	if err := dec.Decode(&l); err != nil || len(l) != 2 {
		t.Fatal(l, err)
	}
	var in sInner
	if err := dec.Decode(&in); err != nil || in.X != 1 || in.Y != "a" {
		t.Fatal(in, err)
	}
	var wrong int
	err := dec.Decode(&wrong)
	var te *UnmarshalTypeError
	if !errors.As(err, &te) {
		t.Fatalf("want type error, got %v", err)
	}
	var p *int
	if err := dec.Decode(&p); err != nil || p == nil || *p != 4 {
		t.Fatal(p, err)
	}
	var se *SyntaxError
	if err := dec.Decode(&p); !errors.As(err, &se) {
		t.Fatalf("want syntax error, got %v", err)
	}
	if err := dec.Decode(&p); err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("error must be sticky, got %v", err)
	}
}
