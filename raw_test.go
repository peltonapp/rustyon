// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"errors"
	"strings"
	"testing"
)

type rawEnvelope struct {
	Kind    string
	Payload Raw
	After   int
}

func TestRawDecodeVerbatim(t *testing.T) {
	src := `(kind: "a", payload: Foo(/* c */ x: [1, 2], y: 'z'), after: 7)`
	var e rawEnvelope
	if err := Unmarshal([]byte(src), &e); err != nil {
		t.Fatal(err)
	}
	if string(e.Payload) != `Foo(/* c */ x: [1, 2], y: 'z')` || e.After != 7 || e.Kind != "a" {
		t.Fatalf("%+v %s", e, e.Payload)
	}
	// The tree decoder prints the subtree instead.
	var viaTree rawEnvelope
	if err := unmarshalTree([]byte(src), &viaTree, &decodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if string(viaTree.Payload) != `Foo(x:[1,2],y:'z')` {
		t.Fatalf("tree: %s", viaTree.Payload)
	}
	// With extensions both paths print the tree.
	var ext rawEnvelope
	if err := Unmarshal([]byte("#![enable(implicit_some)]\n"+src), &ext); err != nil {
		t.Fatal(err)
	}
	if string(ext.Payload) != `Foo(x:[1,2],y:'z')` {
		t.Fatalf("ext: %s", ext.Payload)
	}
}

func TestRawShapes(t *testing.T) {
	cases := []string{
		`5`, `-1.5e3`, `"s\n"`, `'c'`, `b"x"`, `r#"raw"#`, `None`, `Some(5)`, `Some( Some(1) )`, `()`, `( )`, `Unit`,
		`[1, /* x */ 2]`, `{"a": 1}`, `(1, 2)`, `Name(a: 1,)`, `1..5`, `..=3`, `r#a-b`, `[[[]]]`, `Foo(Some(1), None)`,
	}
	for _, c := range cases {
		for _, doc := range []string{c, "  /* a */ " + c + " // end\n"} {
			var r Raw
			if err := Unmarshal([]byte(doc), &r); err != nil {
				t.Fatalf("%q: %v", doc, err)
			}
			if string(r) != c {
				t.Errorf("%q: got %q", doc, r)
			}
			want, _ := Parse([]byte(c))
			got, err := Parse(r)
			if err != nil || !Equal(want, got) {
				t.Errorf("%q: captured text parses to %v, %v", c, got, err)
			}
		}
		// In containers.
		var rs []Raw
		var m map[string]Raw
		var p *Raw
		if err := Unmarshal([]byte("["+c+", "+c+"]"), &rs); err != nil || len(rs) != 2 || string(rs[1]) != c {
			t.Errorf("list of %q: %q %v", c, rs, err)
		}
		if err := Unmarshal([]byte(`{"k": `+c+`}`), &m); err != nil || string(m["k"]) != c {
			t.Errorf("map of %q: %q %v", c, m, err)
		}
		if err := Unmarshal([]byte(`Some(`+c+`)`), &p); err != nil || p == nil || string(*p) != c {
			t.Errorf("option of %q: %v", c, err)
		}
	}
	var r Raw
	if err := Unmarshal([]byte(`[1,`), &r); err == nil {
		t.Error("want a syntax error")
	}
	if err := Unmarshal([]byte(`1 2`), &r); err == nil {
		t.Error("trailing data accepted")
	}
}

func TestRawEncode(t *testing.T) {
	e := rawEnvelope{Kind: "a", Payload: Raw(`Foo( x : [1,2] ) // c`), After: 1}
	out, err := Marshal(e)
	if err != nil || string(out) != `(kind:"a",payload:Foo(x:[1,2]),after:1)` {
		t.Fatalf("%s %v", out, err)
	}
	out, err = MarshalIndent(e, "", "  ")
	if err != nil || !strings.Contains(string(out), "payload: Foo(\n    x: [\n") {
		t.Fatalf("%s %v", out, err)
	}
	out, _ = Marshal(rawEnvelope{})
	if string(out) != `(kind:"",payload:None,after:0)` {
		t.Fatalf("%s", out)
	}
	_, err = Marshal(rawEnvelope{Payload: Raw(`(1,`)})
	var me *MarshalerError
	if !errors.As(err, &me) {
		t.Fatalf("want MarshalerError, got %v", err)
	}
	_, err = Marshal(rawEnvelope{Payload: Raw(``)})
	if !errors.As(err, &me) {
		t.Fatalf("empty raw: %v", err)
	}
	// omitempty skips an empty Raw.
	out, _ = Marshal(struct {
		R Raw `ron:",omitempty"`
	}{})
	if string(out) != `()` {
		t.Fatalf("%s", out)
	}
}

func TestRawDecodeLater(t *testing.T) {
	var e rawEnvelope
	if err := Unmarshal([]byte(`(kind: "p", payload: (x: 3, y: 4))`), &e); err != nil {
		t.Fatal(err)
	}
	var pt struct{ X, Y int }
	if err := e.Payload.Decode(&pt); err != nil || pt.X != 3 || pt.Y != 4 {
		t.Fatal(pt, err)
	}
}

func TestUnmarshalAs(t *testing.T) {
	type cfg struct{ MaxHP int }
	c, err := UnmarshalAs[cfg]([]byte(`(max_hp: 5)`))
	if err != nil || c.MaxHP != 5 {
		t.Fatal(c, err)
	}
	c, err = UnmarshalAs[cfg]([]byte(`(maxHp: 6, extra: 1)`), DecodeOptions{FieldNaming: CamelCase})
	if err != nil || c.MaxHP != 6 {
		t.Fatal(c, err)
	}
	if _, err = UnmarshalAs[cfg]([]byte(`(extra: 1)`), DecodeOptions{DisallowUnknownFields: true}); err == nil {
		t.Fatal("want unknown field error")
	}
	if _, err = UnmarshalAs[int]([]byte(`"x"`)); err == nil {
		t.Fatal("want type error")
	}
	l, err := UnmarshalAs[[]string]([]byte(`["a"]`))
	if err != nil || len(l) != 1 {
		t.Fatal(l, err)
	}

	dec := NewDecoder(strings.NewReader(`1 2`))
	a, err := DecodeAs[int](dec)
	if err != nil || a != 1 {
		t.Fatal(a, err)
	}
	b, err := DecodeAs[int](dec)
	if err != nil || b != 2 {
		t.Fatal(b, err)
	}
	if _, err := DecodeAs[int](dec); err == nil {
		t.Fatal("want EOF")
	}
}
