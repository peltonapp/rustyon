// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package ronjson

import (
	"testing"
)

func TestToJSON(t *testing.T) {
	tests := []struct{ in, want string }{
		{"()", "null"},
		{"None", "null"},
		{"Some(1)", "1"},
		{"true", "true"},
		{"340282366920938463463374607431768211455", "340282366920938463463374607431768211455"},
		{"1.5", "1.5"},
		{`"a\"b"`, `"a\"b"`},
		{"'c'", `"c"`},
		{"Red", `"Red"`},
		{`b"\x01\x02"`, "[1,2]"},
		{"[1, (2, 3)]", "[1,[2,3]]"},
		{"Point(y: 1, x: 2)", `{"y":1,"x":2}`},
		{`{"b": 1, 'a': 2, 3: 4, Key: 5, true: 6}`, `{"b":1,"a":2,"3":4,"Key":5,"true":6}`},
		{"Circle(1.0)", "[1]"},
	}
	for _, tt := range tests {
		got, err := ToJSON([]byte(tt.in), "")
		if err != nil {
			t.Errorf("ToJSON(%q): %v", tt.in, err)
			continue
		}
		if string(got) != tt.want {
			t.Errorf("ToJSON(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestToJSONIndent(t *testing.T) {
	got, err := ToJSON([]byte("(a: [1])"), "  ")
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"a\": [\n    1\n  ]\n}"
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestToJSONErrors(t *testing.T) {
	for _, in := range []string{"NaN", "inf", "{[1]: 2}", "[1"} {
		if _, err := ToJSON([]byte(in), ""); err == nil {
			t.Errorf("ToJSON(%q): expected error", in)
		}
	}
}

func TestFromJSON(t *testing.T) {
	tests := []struct {
		in   string
		opts Options
		want string
	}{
		{"null", Options{}, "None"},
		{"[1, 2.5, 1e3, 18446744073709551616]", Options{}, "[1,2.5,1000.0,18446744073709551616]"},
		{`{"b": true, "a": "x"}`, Options{}, `{"b":true,"a":"x"}`},
		{`{"b": true, "a": "x"}`, Options{Structs: true}, `(b:true,a:"x")`},
		{`{"not ident": 1}`, Options{Structs: true}, `{"not ident":1}`},
		{`{}`, Options{Structs: true}, `{}`},
		{`{"a": [1]}`, Options{Indent: "  "}, "{\n  \"a\": [\n    1,\n  ],\n}"},
	}
	for _, tt := range tests {
		got, err := FromJSON([]byte(tt.in), tt.opts)
		if err != nil {
			t.Errorf("FromJSON(%q): %v", tt.in, err)
			continue
		}
		if string(got) != tt.want {
			t.Errorf("FromJSON(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestFromJSONErrors(t *testing.T) {
	for _, in := range []string{"", "[1,", "1 2", "{\"a\" 1}"} {
		if _, err := FromJSON([]byte(in), Options{}); err == nil {
			t.Errorf("FromJSON(%q): expected error", in)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	src := `{"name":"x","list":[1,2,{"k":null}],"n":12345678901234567890}`
	ron, err := FromJSON([]byte(src), Options{Structs: true})
	if err != nil {
		t.Fatal(err)
	}
	back, err := ToJSON(ron, "")
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != src {
		t.Errorf("round trip: %s -> %s -> %s", src, ron, back)
	}
}
