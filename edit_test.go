// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"errors"
	"strings"
	"testing"
)

func TestEditable(t *testing.T) {
	n := func(v int64) Value { return NewInt(v) }
	tests := []struct {
		name, src string
		op        func(e *Editable) error
		want      string
	}{
		// Set
		{"set field", "(a: 1, b: 2)", func(e *Editable) error { return e.Set("b", n(5)) }, "(a: 1, b: 5)"},
		{
			"set keeps comments around the value",
			"(\n    // about a\n    a: 1, // one\n    /* b */ b: 2, // two\n)",
			func(e *Editable) error { return e.Set("a", String("x")) },
			"(\n    // about a\n    a: \"x\", // one\n    /* b */ b: 2, // two\n)",
		},
		{
			"set drops comments inside the old value",
			"(a: [1, /* inner */ 2], b: 1)",
			func(e *Editable) error { return e.Set("a", n(0)) },
			"(a: 0, b: 1)",
		},
		{"set through nested Some", "(a: Some(Some((b: 1))))", func(e *Editable) error { return e.Set("a.b", n(7)) }, "(a: Some(Some((b: 7))))"},
		{"set replaces the Some itself", "(a: Some(5))", func(e *Editable) error { return e.Set("a", n(7)) }, "(a: 7)"},
		{"set whole document", "// h\n(a: 1) // t", func(e *Editable) error { return e.Set("", String("z")) }, "// h\n\"z\" // t"},
		{"set with extension header", "#![enable(implicit_some)]\n(a: Some 1)", func(e *Editable) error { return e.Set("a", n(2)) }, "#![enable(implicit_some)]\n(a: 2)"},
		{"set tuple element", "Point(1, 2)", func(e *Editable) error { return e.Set("[-1]", n(9)) }, "Point(1, 9)"},
		{"set map entry by int", "{1: 1, (1, 2): 3}", func(e *Editable) error { return e.Set("[1]", n(7)) }, "{1: 7, (1, 2): 3}"},
		{"set map entry by identifier key", "{A: 1}", func(e *Editable) error { return e.Set("A", n(2)) }, "{A: 2}"},
		{"set map entry by string key", "{\"a b\": 1}", func(e *Editable) error { return e.Set(`["a b"]`, n(2)) }, "{\"a b\": 2}"},
		{"set raw identifier field", "(r#type: 1)", func(e *Editable) error { return e.Set("type", n(2)) }, "(r#type: 2)"},
		{"set under unwrapped variant", "#![enable(unwrap_variant_newtypes)]\n(a: Some(x: 1))", func(e *Editable) error { return e.Set("a.x", n(9)) }, "#![enable(unwrap_variant_newtypes)]\n(a: Some(x: 9))"},
		{"setron range", "(a: 1)", func(e *Editable) error { return e.SetRON("a", " 1..5 ") }, "(a: 1..5)"},
		{"setron keeps spelling", "(a: 1)", func(e *Editable) error { return e.SetRON("a", "0xFF") }, "(a: 0xFF)"},

		// Set adds missing fields and keys
		{"add field inline", "(a: 1)", func(e *Editable) error { return e.Set("c", n(3)) }, "(a: 1, c: 3)"},
		{"add field after trailing comma", "(a: 1,)", func(e *Editable) error { return e.Set("c", n(3)) }, "(a: 1, c: 3,)"},
		{
			"add field multi line",
			"(\n    a: 1,\n    b: 2, // two\n)",
			func(e *Editable) error { return e.Set("c", n(3)) },
			"(\n    a: 1,\n    b: 2, // two\n    c: 3,\n)",
		},
		{
			"add field multi line without trailing comma",
			"(\n    a: 1,\n    b: 2 // two\n)",
			func(e *Editable) error { return e.Set("c", n(3)) },
			"(\n    a: 1,\n    b: 2, // two\n    c: 3\n)",
		},
		{
			"add nested field",
			"(\n    cfg: (\n        a: 1,\n    ),\n)",
			func(e *Editable) error { return e.Set("cfg.b", List{n(1)}) },
			"(\n    cfg: (\n        a: 1,\n        b: [1],\n    ),\n)",
		},
		{"add field to empty named", "Cfg()", func(e *Editable) error { return e.Set("c", n(3)) }, "Cfg(c: 3)"},
		{"add field to unit", "(x: (), y: 1)", func(e *Editable) error { return e.Set("x.c", n(3)) }, "(x: (c: 3), y: 1)"},
		{"add raw identifier field", "(a: 1)", func(e *Editable) error { return e.Set("type", n(3)) }, "(a: 1, type: 3)"},
		{"add reserved field", "(a: 1)", func(e *Editable) error { return e.Set("true", n(3)) }, "(a: 1, r#true: 3)"},
		{"add map key string", "{\"a\": 1}", func(e *Editable) error { return e.Set("b", n(3)) }, "{\"a\": 1, \"b\": 3}"},
		{"add map key identifier", "{A: 1}", func(e *Editable) error { return e.Set("B", n(3)) }, "{A: 1, B: 3}"},
		{"add map key integer", "{1: 1}", func(e *Editable) error { return e.Set("[2]", n(3)) }, "{1: 1, 2: 3}"},
		{"add map key to empty map", "{}", func(e *Editable) error { return e.Set("k", n(3)) }, "{\"k\": 3}"},
		{
			"add map key multi line",
			"{\n    \"a\": 1, // one\n}",
			func(e *Editable) error { return e.Set(`["b"]`, n(2)) },
			"{\n    \"a\": 1, // one\n    \"b\": 2,\n}",
		},
		{
			"add field to empty with comment",
			"(\n    // nothing yet\n)",
			func(e *Editable) error { return e.Set("a", n(1)) },
			"(\n    // nothing yet\n    a: 1,\n)",
		},

		// Delete
		{"delete middle", "[1, 2, 3]", func(e *Editable) error { return e.Delete("[1]") }, "[1, 3]"},
		{"delete last", "[1, 2, 3]", func(e *Editable) error { return e.Delete("[-1]") }, "[1, 2]"},
		{"delete first", "[1, 2, 3]", func(e *Editable) error { return e.Delete("[0]") }, "[2, 3]"},
		{"delete the only element", "[1]", func(e *Editable) error { return e.Delete("[0]") }, "[]"},
		{"delete last with trailing comma", "[1, 2,]", func(e *Editable) error { return e.Delete("[1]") }, "[1]"},
		{"delete tuple element", "(1, 2, 3)", func(e *Editable) error { return e.Delete("[1]") }, "(1, 3)"},
		{
			"delete takes attached comments and keeps the rest",
			"[\n    1,\n    // two\n    2, // t\n\n    3,\n]",
			func(e *Editable) error { return e.Delete("[1]") },
			"[\n    1,\n\n    3,\n]",
		},
		{
			"delete keeps a comment separated by a blank line",
			"[\n    1,\n    // about the list\n\n    // two\n    2,\n]",
			func(e *Editable) error { return e.Delete("[1]") },
			"[\n    1,\n    // about the list\n\n]",
		},
		{
			"delete last multi line",
			"[\n    1,\n    2,\n    3,\n]",
			func(e *Editable) error { return e.Delete("[2]") },
			"[\n    1,\n    2,\n]",
		},
		{
			"delete last multi line without trailing comma",
			"[\n    1,\n    2,\n    3\n]",
			func(e *Editable) error { return e.Delete("[2]") },
			"[\n    1,\n    2,\n]",
		},
		{"delete only element multi line", "[\n    1,\n]", func(e *Editable) error { return e.Delete("[0]") }, "[]"},
		{
			"delete only element keeps a free comment",
			"[\n    // keep\n\n    1,\n]",
			func(e *Editable) error { return e.Delete("[0]") },
			"[\n    // keep\n\n]",
		},
		{"delete field", "(a: 1, b: 2)", func(e *Editable) error { return e.Delete("a") }, "(b: 2)"},
		{
			"delete field multi line",
			"Cfg(\n    a: 1, // a\n    b: 2,\n)",
			func(e *Editable) error { return e.Delete("b") },
			"Cfg(\n    a: 1, // a\n)",
		},
		{"delete only field", "Cfg(a: 1)", func(e *Editable) error { return e.Delete("a") }, "Cfg()"},
		{"delete map entry", "{\"a\": 1, 2: 3}", func(e *Editable) error { return e.Delete("[2]") }, "{\"a\": 1}"},
		{"delete keeps comment of previous element", "[1 /*p*/, 2]", func(e *Editable) error { return e.Delete("[1]") }, "[1 /*p*/,]"},
		{"delete through Some", "(a: Some([1, 2]))", func(e *Editable) error { return e.Delete("a[0]") }, "(a: Some([2]))"},
		{"delete keeps header and trailing comments", "// h\n[1, 2] // t", func(e *Editable) error { return e.Delete("[0]") }, "// h\n[2] // t"},

		// Insert and Append
		{"append inline", "[1, 2]", func(e *Editable) error { return e.Append("", n(3)) }, "[1, 2, 3]"},
		{"append inline with trailing comma", "[1, 2,]", func(e *Editable) error { return e.Append("", n(3)) }, "[1, 2, 3,]"},
		{
			"append multi line",
			"[\n    1,\n    2,\n]",
			func(e *Editable) error { return e.Append("", n(3)) },
			"[\n    1,\n    2,\n    3,\n]",
		},
		{
			"append after a trailing comment",
			"[\n    1,\n    2 // x\n]",
			func(e *Editable) error { return e.Append("", n(3)) },
			"[\n    1,\n    2, // x\n    3\n]",
		},
		{
			"append after a line comment on a shared line",
			"[1, 2, // x\n]",
			func(e *Editable) error { return e.Append("", n(3)) },
			"[1, 2, // x\n3,\n]",
		},
		{"append to empty", "[]", func(e *Editable) error { return e.Append("", n(3)) }, "[3]"},
		{"append to empty multi line", "(a: [\n])", func(e *Editable) error { return e.Append("a", n(3)) }, "(a: [\n    3,\n])"},
		{
			"append to empty with comment",
			"(\n  a: [\n    // none\n  ],\n)",
			func(e *Editable) error { return e.Append("a", n(3)) },
			"(\n  a: [\n    // none\n    3,\n  ],\n)",
		},
		{"insert in the middle", "[1, 3]", func(e *Editable) error { return e.Insert("", 1, n(2)) }, "[1, 2, 3]"},
		{"insert at the front", "[2, 3]", func(e *Editable) error { return e.Insert("", 0, n(1)) }, "[1, 2, 3]"},
		{
			"insert keeps comments with their element",
			"[\n    1,\n    // three\n    3,\n]",
			func(e *Editable) error { return e.Insert("", 1, n(2)) },
			"[\n    1,\n    2,\n    // three\n    3,\n]",
		},
		{"append through Some", "(a: Some([1]))", func(e *Editable) error { return e.Append("a", n(2)) }, "(a: Some([1, 2]))"},
		{"append a struct", "[]", func(e *Editable) error { return e.Append("", Struct{Fields: []Field{{"x", n(1)}}}) }, "[(x:1)]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := ParseEditable([]byte(tt.src))
			if err != nil {
				t.Fatal(err)
			}
			if err := tt.op(e); err != nil {
				t.Fatal(err)
			}
			if got := e.String(); got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
			if string(e.Bytes()) != e.String() {
				t.Error("Bytes and String differ")
			}
			if _, err := Parse(e.Bytes()); err != nil {
				t.Errorf("result does not parse: %v", err)
			}
		})
	}
}

func TestEditableGet(t *testing.T) {
	src := `#![enable(implicit_some)]
// config
(
    items: [(name: "a"), (name: "b")],
    nested: Some(Some((x: 7))),
    named: Point(1, 2),
    m: {"k": 1, Id: 2, 3: 4, (1, 2): 5},
    r: 1..5,
    n: None,
)`
	e, err := ParseEditable([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path string
		want Value
	}{
		{"items[0].name", String("a")},
		{".items[-1].name", String("b")},
		{`items[1]["name"]`, String("b")},
		{"nested.x", NewInt(7)},
		{"named[1]", NewInt(2)},
		{"m.k", NewInt(1)},
		{"m.Id", NewInt(2)},
		{"m[3]", NewInt(4)},
		{`m["Id"]`, NewInt(2)},
		{"r", Map{{String("start"), NewInt(1)}, {String("end"), NewInt(5)}}},
		{"n", None},
	}
	for _, tt := range tests {
		got, ok := e.Get(tt.path)
		if !ok || !Equal(got, tt.want) {
			t.Errorf("Get(%q) = %v, %v; want %v", tt.path, got, ok, tt.want)
		}
	}
	whole, ok := e.Get(".")
	want, _ := Parse([]byte(src))
	if !ok || !Equal(whole, want) {
		t.Errorf("Get(.) = %v, %v", whole, ok)
	}
	for _, p := range []string{"missing", "items[2]", "items.x", "n.x", "named.x", "m[9]", "a..b", "m[(1, 2)]"} {
		if v, ok := e.Get(p); ok {
			t.Errorf("Get(%q) = %v, want not found", p, v)
		}
	}
	if e.Extensions() != ImplicitSome {
		t.Errorf("Extensions() = %v", e.Extensions())
	}
}

func TestEditableGetSeesEdits(t *testing.T) {
	e, err := ParseEditable([]byte("(a: [1, 2])"))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Append("a", NewInt(3)); err != nil {
		t.Fatal(err)
	}
	if err := e.Set("b", String("x")); err != nil {
		t.Fatal(err)
	}
	if v, ok := e.Get("a[2]"); !ok || !Equal(v, NewInt(3)) {
		t.Errorf("a[2] = %v, %v", v, ok)
	}
	if v, ok := e.Get("b"); !ok || !Equal(v, String("x")) {
		t.Errorf("b = %v, %v", v, ok)
	}
	if err := e.Delete("a[0]"); err != nil {
		t.Fatal(err)
	}
	if v, ok := e.Get("a[0]"); !ok || !Equal(v, NewInt(2)) {
		t.Errorf("a[0] = %v, %v", v, ok)
	}
}

func TestEditableErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		op   func(e *Editable) error
		is   error
	}{
		{"set missing parent", "(a: 1)", func(e *Editable) error { return e.Set("x.y", NewInt(1)) }, ErrPathNotFound},
		{"set missing list index", "[1]", func(e *Editable) error { return e.Set("[3]", NewInt(1)) }, ErrPathNotFound},
		{"set field of a list", "[1]", func(e *Editable) error { return e.Set("a", NewInt(1)) }, ErrPathNotFound},
		{"set field of None", "(a: None)", func(e *Editable) error { return e.Set("a.b", NewInt(1)) }, ErrPathNotFound},
		{"set field of a tuple", "(1, 2)", func(e *Editable) error { return e.Set("a", NewInt(1)) }, ErrPathNotFound},
		{"set field of a scalar", "(a: 1)", func(e *Editable) error { return e.Set("a.b", NewInt(1)) }, ErrPathNotFound},
		{"delete missing", "(a: 1)", func(e *Editable) error { return e.Delete("b") }, ErrPathNotFound},
		{"delete missing index", "[1]", func(e *Editable) error { return e.Delete("[1]") }, ErrPathNotFound},
		{"delete root", "(a: 1)", func(e *Editable) error { return e.Delete("") }, nil},
		{"bad path", "(a: 1)", func(e *Editable) error { return e.Set("a..b", NewInt(1)) }, nil},
		{"setron not a value", "(a: 1)", func(e *Editable) error { return e.SetRON("a", "1 2") }, nil},
		{"setron comment", "(a: 1)", func(e *Editable) error { return e.SetRON("a", "1 // c") }, nil},
		{"setron needs the extension", "(a: 1)", func(e *Editable) error { return e.SetRON("a", "Some 1") }, nil},
		{"insert out of range", "[1]", func(e *Editable) error { return e.Insert("", 2, NewInt(1)) }, nil},
		{"insert into a map", "{}", func(e *Editable) error { return e.Append("", NewInt(1)) }, nil},
		{"insert missing path", "(a: [])", func(e *Editable) error { return e.Append("b", NewInt(1)) }, ErrPathNotFound},
		{"invalid identifier", "(a: 1)", func(e *Editable) error { return e.Set("a b", NewInt(1)) }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := ParseEditable([]byte(tt.src))
			if err != nil {
				t.Fatal(err)
			}
			err = tt.op(e)
			if err == nil {
				t.Fatalf("expected an error, document is now %q", e.String())
			}
			if tt.is != nil && !errors.Is(err, tt.is) {
				t.Errorf("error %v does not wrap %v", err, tt.is)
			}
			if e.String() != tt.src {
				t.Errorf("document changed to %q", e.String())
			}
		})
	}
	if _, err := ParseEditable([]byte("(a: ")); err == nil {
		t.Error("ParseEditable accepted invalid input")
	}
}

func TestEditableFormat(t *testing.T) {
	src := "// c\n(a:1,b:[1,2], /* k */ c:3)"
	e, err := ParseEditable([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Set("a", NewInt(9)); err != nil {
		t.Fatal(err)
	}
	got := string(e.Format("  "))
	want, _ := FormatIndent([]byte(e.String()), "  ")
	if got != string(want) {
		t.Errorf("Format differs from FormatIndent:\n%s\n%s", got, want)
	}
	if !strings.Contains(got, "/* k */") || !strings.Contains(got, "// c") {
		t.Errorf("comments lost: %s", got)
	}
}

func TestEditableBOMAndCRLF(t *testing.T) {
	src := "\xEF\xBB\xBF(\r\n    a: 1,\r\n    b: 2,\r\n)\r\n"
	e, err := ParseEditable([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if err := e.Set("a", NewInt(5)); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(e.Bytes()); err != nil {
		t.Fatalf("%q: %v", e.String(), err)
	}
	if v, ok := e.Get("a"); !ok || !Equal(v, NewInt(5)) {
		t.Errorf("a = %v, %v", v, ok)
	}
}

// TestEditableLeavesOtherTextAlone checks that an edit changes nothing
// outside the edited element.
func TestEditableLeavesOtherTextAlone(t *testing.T) {
	src := "#![enable(implicit_some)]\n// head\n(\n    a: 0x10, // hex\n    b  :  1_0 ,\n    /* c */ c: r#\"raw\"#,\n)\n// tail\n"
	e, err := ParseEditable([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Set("b", NewInt(2)); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(src, "1_0", "2", 1)
	if e.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", e.String(), want)
	}
}
