// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"os"
	"path/filepath"
	"testing"
)

var fuzzSeeds = []string{
	"()",
	"[1, 2, 3]",
	`{"a": (x: 1, y: -2.5e3), 'c': b"\x00"}`,
	"Some(Point(1, 2))",
	"#![enable(implicit_some)]\n(a: None, b: r#\"raw\"#)",
	"// c\n( /* b */ a: [\n1, // t\n],\n)",
	"0x_ff_u8",
	"-inf",
	"(r#type: r#true)",
	"[[[[[]]]]]",
	"'\\u{1F600}'",
	"[1..5, ..=3, 2.., .., 1.5..2.5, inf..NaN]",
	"#![enable(implicit_some)]\n[Some 1, Some(2), Some /* c */ 3, None]",
	"#![enable(unwrap_variant_newtypes)]\n[Some(x: 1, y: 2), Some(1, 2), Some(), Some((1, 2))]",
	"#![type = \"my::Type\"]\n#![schema = r#\"s.ron\"#]\n// c\n( )",
	"#![enable(implicit_some, /* c */ unwrap_newtypes)] Foo( /* a */ 1 )",
	"Name /* c */ (a /* k */ : /* v */ 1,)",
	"{ (1, 2): [1], Key: r#raw.id-1, 3: b'a', \"s\": b\"x\" }",
	"(a: 0b10_1u8, b: -0o7, c: 1e-3f32, d: 1_0.5_0, e: +4, f: -NaN)",
	"( /* only */ )",
	"Unit",
}

func addSeeds(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add([]byte(s))
	}
	files, _ := filepath.Glob("testdata/format/*.ron")
	for _, name := range files {
		if b, err := os.ReadFile(name); err == nil {
			f.Add(b)
		}
	}
}

// FuzzParse checks that the parser never panics and that every parsed value
// survives a print and parse round trip.
func FuzzParse(f *testing.F) {
	addSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		v, err := Parse(data)
		if err != nil {
			return
		}
		for _, indent := range []string{"", "  "} {
			out, err := printDocument(v, &encodeOptions{indent: indent})
			if err != nil {
				return // identifiers that cannot be printed
			}
			v2, err := Parse(out)
			if err != nil {
				t.Fatalf("printed value does not parse: %v\ninput: %q\noutput: %q", err, data, out)
			}
			if !Equal(v, v2) {
				t.Fatalf("round trip changed value\ninput: %q\noutput: %q", data, out)
			}
		}
	})
}

// FuzzFormat checks that Format accepts exactly what Parse accepts, is
// idempotent and preserves the value.
func FuzzFormat(f *testing.F) {
	addSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		out, ferr := Format(data)
		v1, perr := Parse(data)
		if (ferr == nil) != (perr == nil) {
			t.Fatalf("Format and Parse disagree on %q: format error %v, parse error %v", data, ferr, perr)
		}
		if ferr != nil {
			return
		}
		v2, err := Parse(out)
		if err != nil {
			t.Fatalf("formatted output does not parse: %v\ninput: %q\noutput: %q", err, data, out)
		}
		if !Equal(v1, v2) {
			t.Fatalf("Format changed the value\ninput: %q\noutput: %q", data, out)
		}
		again, err := Format(out)
		if err != nil {
			t.Fatal(err)
		}
		if string(again) != string(out) {
			t.Fatalf("Format not idempotent\ninput: %q\nfirst: %q\nsecond: %q", data, out, again)
		}
	})
}

// FuzzUnmarshal checks that decoding arbitrary input into a typed value
// never panics.
func FuzzUnmarshal(f *testing.F) {
	addSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		var a any
		_ = Unmarshal(data, &a)
		var s struct {
			A []int
			B map[string]*float64
			C tupleStruct
			D [2]string
			E Value
		}
		_ = Unmarshal(data, &s)
	})
}
