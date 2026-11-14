// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

func TestFormatGolden(t *testing.T) {
	inputs, err := filepath.Glob("testdata/format/*.ron")
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range inputs {
		if strings.HasSuffix(in, ".golden.ron") {
			continue
		}
		t.Run(filepath.Base(in), func(t *testing.T) {
			src, err := os.ReadFile(in)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Format(src)
			if err != nil {
				t.Fatal(err)
			}
			golden := strings.TrimSuffix(in, ".ron") + ".golden.ron"
			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Errorf("Format mismatch\ngot:\n%s\nwant:\n%s", got, want)
			}
			again, err := Format(got)
			if err != nil {
				t.Fatal(err)
			}
			if string(again) != string(got) {
				t.Errorf("Format is not idempotent\nfirst:\n%s\nsecond:\n%s", got, again)
			}
			// Formatting must not change the meaning of the document.
			a, _ := Parse(src)
			b, err := Parse(got)
			if err != nil {
				t.Fatal(err)
			}
			if !Equal(a, b) {
				t.Error("formatted document has a different value")
			}
		})
	}
}

func TestFormat(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"scalar", "  42  ", "42\n"},
		{"inline list", "[1,2,  3]", "[1, 2, 3]\n"},
		{"inline trailing comma dropped", "[1, 2,]", "[1, 2]\n"},
		{"multiline list", "[1,\n2]", "[\n    1,\n    2,\n]\n"},
		{"inline struct", "Point(x:1,y:2)", "Point(x: 1, y: 2)\n"},
		{"map", "{\"a\":1}", "{\"a\": 1}\n"},
		{"empty", "( )", "()\n"},
		{"hug", "Some((a: 1))", "Some((a: 1))\n"},
		{"hug multiline", "Some([\n1])", "Some([\n    1,\n])\n"},
		{"no hug when split", "Some(\n[1])", "Some(\n    [1],\n)\n"},
		{"comment forces expansion", "[1, /* c */ 2]", "[\n    1, /* c */\n    2,\n]\n"},
		{"own line block comment", "[1,\n/* c */ 2]", "[\n    1,\n    /* c */\n    2,\n]\n"},
		{"trailing line comment", "[1, // one\n2]", "[\n    1, // one\n    2,\n]\n"},
		{"line comment before comma", "[1 // a\n, 2]", "[\n    1, // a\n    2,\n]\n"},
		{"two line comments", "[1 // a\n, // b\n2]", "[\n    1, // a\n    // b\n    2,\n]\n"},
		{"stray comments", "(a /* k */ : /* v */ 1)", "(\n    /* k */\n    /* v */\n    a: 1,\n)\n"},
		{"comment after name", "Some /* c */ (1)", "Some(\n    /* c */\n    1,\n)\n"},
		{"blank lines collapsed", "[\n1,\n\n\n\n2]", "[\n    1,\n\n    2,\n]\n"},
		{"extensions normalized", "#![enable( implicit_some,unwrap_newtypes )] 1", "#![enable(implicit_some, unwrap_newtypes)]\n1\n"},
		{"comment only header", "// a\n\n// b\n1 // c\n// d", "// a\n\n// b\n1 // c\n// d\n"},
		{"block comment nested", "/* a /* b */ c */ 1", "/* a /* b */ c */\n1\n"},
		{"literals kept", "[0xFF, 1_0, r\"x\", 'a', b\"\\x00\", 1e3, -inf]", "[0xFF, 1_0, r\"x\", 'a', b\"\\x00\", 1e3, -inf]\n"},
		{"trailing whitespace in comment", "1 // c   ", "1 // c\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Format([]byte(tt.in))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("Format(%q)\ngot:\n%s\nwant:\n%s", tt.in, got, tt.want)
			}
			again, err := Format(got)
			if err != nil {
				t.Fatal(err)
			}
			if string(again) != string(got) {
				t.Errorf("not idempotent:\n%s\n%s", got, again)
			}
		})
	}
}

func TestFormatIndent(t *testing.T) {
	got, err := FormatIndent([]byte("[\n(a: 1,\n)]"), "\t")
	if err != nil {
		t.Fatal(err)
	}
	want := "[\n\t(\n\t\ta: 1,\n\t),\n]\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatErrors(t *testing.T) {
	for _, src := range []string{"[1", "(a: )", "#![enable(nope)] 1", "1 2", "\"open"} {
		if _, err := Format([]byte(src)); err == nil {
			t.Errorf("Format(%q): expected error", src)
		}
	}
}
