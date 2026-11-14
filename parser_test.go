// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"
)

func mustBig(s string) Int {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic(s)
	}
	return NewBigInt(n)
}

func TestParseLiterals(t *testing.T) {
	tests := []struct {
		src  string
		want Value
	}{
		{"()", Unit{}},
		{"true", Bool(true)},
		{"false", Bool(false)},
		{"0", NewInt(0)},
		{"42", NewInt(42)},
		{"-42", NewInt(-42)},
		{"+7", NewInt(7)},
		{"1_000_000", NewInt(1000000)},
		{"0x2A", NewInt(42)},
		{"-0xff", NewInt(-255)},
		{"0o17", NewInt(15)},
		{"0b1010_1010", NewInt(170)},
		{"007", NewInt(7)},
		{"255u8", NewInt(255)},
		{"-128i8", NewInt(-128)},
		{"0xffu8", NewInt(255)},
		{"340282366920938463463374607431768211455u128", mustBig("340282366920938463463374607431768211455")},
		{"-170141183460469231731687303715884105728", mustBig("-170141183460469231731687303715884105728")},
		{"b'a'", NewInt('a')},
		{"b'\\n'", NewInt('\n')},
		{"1.5", Float(1.5)},
		{"1.", Float(1)},
		{".5", Float(0.5)},
		{"-.5", Float(-0.5)},
		{"1e3", Float(1000)},
		{"1E-3", Float(0.001)},
		{"2.5e+2", Float(250)},
		{"1_000.000_1", Float(1000.0001)},
		{"3f32", Float(3)},
		{"1.5f64", Float(1.5)},
		{"inf", Float(math.Inf(1))},
		{"+inf", Float(math.Inf(1))},
		{"-inf", Float(math.Inf(-1))},
		{"NaN", Float(math.NaN())},
		{"inff32", Float(math.Inf(1))},
		{"NaNf64", Float(math.NaN())},
		{"r#inff32", Ident("inff32")},
		{"-NaN", Float(math.NaN())},
		{`"hello"`, String("hello")},
		{`""`, String("")},
		{`"a\nb\tc\\d\"e\'f\0"`, String("a\nb\tc\\d\"e'f\x00")},
		{`"\u{1F600}"`, String("\U0001F600")},
		{`"é"`, String("é")},
		{`"\x41"`, String("A")},
		{`"multi
line"`, String("multi\nline")},
		{"\"cont\\\n    inued\"", String("continued")},
		{`"héllo wörld"`, String("héllo wörld")},
		{`r"raw \n string"`, String(`raw \n string`)},
		{`r#"has "quotes""#`, String(`has "quotes"`)},
		{`r##"has "# inside"##`, String(`has "# inside`)},
		{`'a'`, Char('a')},
		{`'\''`, Char('\'')},
		{`'\u{263A}'`, Char('☺')},
		{`'ß'`, Char('ß')},
		{`b"bytes"`, Bytes("bytes")},
		{`b"\x00\xff"`, Bytes{0, 0xff}},
		{`br"raw\x"`, Bytes(`raw\x`)},
		{`br#"a"b"#`, Bytes(`a"b`)},
		{"None", None},
		{"Some(1)", Some(NewInt(1))},
		{"Some(1,)", Some(NewInt(1))},
		{"Some(None)", Some(None)},
		{"Red", Ident("Red")},
		{"r#true", Ident("true")},
		{"r#foo-bar.baz", Ident("foo-bar.baz")},
		{"\xEF\xBB\xBF1", NewInt(1)},
	}
	for _, tt := range tests {
		got, err := Parse([]byte(tt.src))
		if err != nil {
			t.Errorf("Parse(%q): %v", tt.src, err)
			continue
		}
		if !Equal(got, tt.want) {
			t.Errorf("Parse(%q) = %#v, want %#v", tt.src, got, tt.want)
		}
	}
}

func TestParseCollections(t *testing.T) {
	tests := []struct {
		src  string
		want Value
	}{
		{"[]", List{}},
		{"[1, 2, 3]", List{NewInt(1), NewInt(2), NewInt(3)}},
		{"[1, 2, 3,]", List{NewInt(1), NewInt(2), NewInt(3)}},
		{"{}", Map{}},
		{`{"a": 1, 2: [true]}`, Map{{String("a"), NewInt(1)}, {NewInt(2), List{Bool(true)}}}},
		{"(1, \"x\")", Tuple{Values: []Value{NewInt(1), String("x")}}},
		{"(1,)", Tuple{Values: []Value{NewInt(1)}}},
		{"Point(1, 2)", Tuple{Name: "Point", Values: []Value{NewInt(1), NewInt(2)}}},
		{"Empty()", Tuple{Name: "Empty"}},
		{"(x: 1, y: 2)", Struct{Fields: []Field{{"x", NewInt(1)}, {"y", NewInt(2)}}}},
		{"Point(x: 1, y: 2,)", Struct{Name: "Point", Fields: []Field{{"x", NewInt(1)}, {"y", NewInt(2)}}}},
		{"(r#type: 1)", Struct{Fields: []Field{{"type", NewInt(1)}}}},
		{"(Some: 1)", Struct{Fields: []Field{{"Some", NewInt(1)}}}},
		{"((1, 2), [(a: ())])", Tuple{Values: []Value{
			Tuple{Values: []Value{NewInt(1), NewInt(2)}},
			List{Struct{Fields: []Field{{"a", Unit{}}}}},
		}}},
		{"( // comment\n a /* inline */ : 1 /* nested /* block */ */ )", Struct{Fields: []Field{{"a", NewInt(1)}}}},
	}
	for _, tt := range tests {
		got, err := Parse([]byte(tt.src))
		if err != nil {
			t.Errorf("Parse(%q): %v", tt.src, err)
			continue
		}
		if !Equal(got, tt.want) {
			t.Errorf("Parse(%q) = %#v, want %#v", tt.src, got, tt.want)
		}
	}
}

func TestParseExtensions(t *testing.T) {
	doc, err := ParseDocument([]byte("#![enable(implicit_some)]\n#![enable(unwrap_newtypes, unwrap_variant_newtypes,)]\n// c\n5"))
	if err != nil {
		t.Fatal(err)
	}
	want := ImplicitSome | UnwrapNewtypes | UnwrapVariantNewtypes
	if doc.Extensions != want {
		t.Errorf("extensions = %v, want %v", doc.Extensions, want)
	}
	if got := strings.Join(doc.Extensions.Names(), ","); got != "implicit_some,unwrap_newtypes,unwrap_variant_newtypes" {
		t.Errorf("Names() = %q", got)
	}
	if !Equal(doc.Value, NewInt(5)) {
		t.Errorf("value = %#v", doc.Value)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		src     string
		line    int
		col     int
		message string
	}{
		{"", 1, 1, "expected value"},
		{"[1, 2", 1, 6, "expected ',' or ']'"},
		{"[1 2]", 1, 4, "expected ',' or ']'"},
		{strings.Repeat("9", 60), 1, 1, "does not fit into 128 bits"},
		{"-170141183460469231731687303715884105729", 1, 1, "does not fit into 128 bits"},
		{"(a: 1, 2)", 1, 8, "expected field name"},
		{"{1 2}", 1, 4, "expected ':'"},
		{"1 2", 1, 3, "expected end of input"},
		{`"abc`, 1, 1, "unterminated string"},
		{"'ab'", 1, 1, "exactly one character"},
		{"''", 1, 1, "empty"},
		{`"\q"`, 1, 2, "unknown escape"},
		{`"\xff"`, 1, 2, "out of range"},
		{"/* never closed", 1, 1, "unterminated block comment"},
		{"256u8", 1, 1, "out of range"},
		{"-1u32", 1, 1, "out of range"},
		{"1.5u8", 1, 1, "integer suffix"},
		{"12abc", 1, 1, "invalid number suffix"},
		{"0x", 1, 1, "missing digits"},
		{"0b102", 1, 1, "invalid digit"},
		{"-foo", 1, 1, "expected number after sign"},
		{"#![enable(bogus)] 1", 1, 11, "unknown extension"},
		{"#![disable(x)] 1", 1, 4, "expected enable"},
		{"Some 1", 1, 6, "expected '('"},
		{"\n\n  @", 3, 3, "unexpected character"},
		{"[\"a\xffb\"]", 1, 4, "invalid UTF-8"},
		{`b"é"`, 1, 3, "non-ASCII"},
		{`"\u{110000}"`, 1, 2, "invalid unicode"},
		{`r#"open`, 1, 1, "unterminated raw string"},
		{strings.Repeat("[", maxDepth+1), 1, maxDepth + 1, "nesting depth"},
	}
	for _, tt := range tests {
		_, err := Parse([]byte(tt.src))
		var se *SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("Parse(%q): got %v, want SyntaxError", tt.src, err)
			continue
		}
		if se.Pos.Line != tt.line || se.Pos.Column != tt.col || !strings.Contains(se.Msg, tt.message) {
			t.Errorf("Parse(%q): got %d:%d %q, want %d:%d containing %q", tt.src, se.Pos.Line, se.Pos.Column, se.Msg, tt.line, tt.col, tt.message)
		}
	}
}

func TestValid(t *testing.T) {
	if !Valid([]byte("(a: [1, 2])")) {
		t.Error("Valid returned false for valid input")
	}
	if Valid([]byte("(a: [1, 2)")) {
		t.Error("Valid returned true for invalid input")
	}
}

func TestSyntaxErrorString(t *testing.T) {
	_, err := Parse([]byte("[1,\n  ?]"))
	want := "rustyon: syntax error at 2:3: unexpected character '?'"
	if err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}
}
