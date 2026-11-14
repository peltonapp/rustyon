// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"bytes"
	"strings"
	"testing"
)

const prettyDoc = `(name: "é\n", ch: 'ü', list: [1, 2, 3], nested: (a: [(x: 1)], m: {"k": (1, 2)}), t: (1, 2), empty: [])`

func prettyOf(t *testing.T, cfg PrettyConfig) string {
	t.Helper()
	v, err := Parse([]byte(prettyDoc))
	if err != nil {
		t.Fatal(err)
	}
	out, err := MarshalWithConfig(v, cfg)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(out)
	if err != nil || !Equal(v, back) {
		t.Fatalf("output does not round trip (%v):\n%s", err, out)
	}
	return string(out)
}

func TestPrettyConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  PrettyConfig
		want string
	}{
		{"default", PrettyConfig{}, `(
    name: "é\n",
    ch: 'ü',
    list: [
        1,
        2,
        3,
    ],
    nested: (
        a: [
            (
                x: 1,
            ),
        ],
        m: {
            "k": (1, 2),
        },
    ),
    t: (1, 2),
    empty: [],
)`},
		{"compact arrays", PrettyConfig{CompactArrays: true, Indent: "  "}, `(
  name: "é\n",
  ch: 'ü',
  list: [1, 2, 3],
  nested: (
    a: [(x: 1)],
    m: {
      "k": (1, 2),
    },
  ),
  t: (1, 2),
  empty: [],
)`},
		{"separate tuples", PrettyConfig{SeparateTupleMembers: true, Indent: "\t", DepthLimit: 2}, "(\n\tname: \"é\\n\",\n\tch: 'ü',\n\tlist: [\n\t\t1,\n\t\t2,\n\t\t3,\n\t],\n\tnested: (\n\t\ta: [(x: 1)],\n\t\tm: {\"k\": (1, 2)},\n\t),\n\tt: (\n\t\t1,\n\t\t2,\n\t),\n\tempty: [],\n)"},
		{"escape", PrettyConfig{EscapeNonASCII: true, CompactStructs: true, NewLine: "\r\n", Separator: "  "}, `(name:  "\u{e9}\n", ch:  '\u{fc}', list:  [1, 2, 3], nested:  (a:  [(x:  1)], m:  {"k":  (1, 2)}), t:  (1, 2), empty:  [])`},
		{"enumerate", PrettyConfig{EnumerateArrays: true, DepthLimit: 1}, `(
    name: "é\n",
    ch: 'ü',
    list: [1, 2, 3],
    nested: (a: [(x: 1)], m: {"k": (1, 2)}),
    t: (1, 2),
    empty: [],
)`},
	}
	for _, c := range cases {
		got := prettyOf(t, c.cfg)
		if got != c.want {
			t.Errorf("%s:\n got:\n%s\nwant:\n%s", c.name, got, c.want)
		}
	}

	// Enumerated arrays at the top level.
	out, err := MarshalWithConfig([]int{5, 6}, PrettyConfig{EnumerateArrays: true})
	if err != nil || string(out) != "[\n    // [0]\n    5,\n    // [1]\n    6,\n]" {
		t.Errorf("%q %v", out, err)
	}
	if v, err := Parse(out); err != nil || !Equal(v, List{NewInt(5), NewInt(6)}) {
		t.Errorf("enumerated output does not parse back: %v", err)
	}
	out, _ = MarshalWithConfig([]int{5, 6}, PrettyConfig{Prefix: "  ", NewLine: "\r\n"})
	if string(out) != "[\r\n      5,\r\n      6,\r\n  ]" {
		t.Errorf("%q", out)
	}
}

func TestPrettyConfigValidation(t *testing.T) {
	bad := []PrettyConfig{
		{Indent: "x"}, {Prefix: "//"}, {NewLine: "\n/"}, {Separator: "."},
		{EnumerateArrays: true, NewLine: " "}, {DepthLimit: -1},
	}
	for _, c := range bad {
		if _, err := MarshalWithConfig([]int{1}, c); err == nil {
			t.Errorf("%+v: want an error", c)
		}
	}
}

func TestPrettyEncoderAndExtensions(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	enc.SetImplicitSome(true)
	enc.SetPrettyConfig(&PrettyConfig{CompactArrays: true})
	if err := enc.Encode(struct{ A []int }{[]int{1, 2}}); err != nil {
		t.Fatal(err)
	}
	want := "#![enable(implicit_some)]\n(\n    a: [1, 2],\n)\n"
	if buf.String() != want {
		t.Fatalf("%q", buf.String())
	}
	buf.Reset()
	enc.SetPrettyConfig(nil)
	if err := enc.Encode([]int{1}); err != nil || !strings.HasSuffix(buf.String(), "[1]\n") {
		t.Fatal(buf.String(), err)
	}
	enc.SetPrettyConfig(&PrettyConfig{})
	enc.SetIndent("", "")
	buf.Reset()
	if err := enc.Encode([]int{1, 2}); err != nil || !strings.HasSuffix(buf.String(), "[1,2]\n") {
		t.Fatal(buf.String(), err)
	}
}

func prettyConfigFromBits(b uint16, depth uint8) PrettyConfig {
	c := PrettyConfig{
		CompactArrays:        b&1 != 0,
		CompactStructs:       b&2 != 0,
		CompactMaps:          b&4 != 0,
		SeparateTupleMembers: b&8 != 0,
		EnumerateArrays:      b&16 != 0,
		EscapeNonASCII:       b&32 != 0,
		DepthLimit:           int(depth % 5),
	}
	if b&64 != 0 {
		c.Indent = "\t"
	}
	if b&128 != 0 {
		c.NewLine = "\r\n"
	}
	if b&256 != 0 {
		c.Separator = "\n  "
	}
	if b&512 != 0 {
		c.Prefix = " \t"
	}
	return c
}

// checkPretty prints v with the config and checks that the output parses
// back to the same value.
func checkPretty(t testing.TB, v Value, cfg PrettyConfig) {
	t.Helper()
	out, err := MarshalWithConfig(v, cfg)
	if err != nil {
		return // identifiers that cannot be printed
	}
	back, err := Parse(out)
	if err != nil {
		t.Fatalf("output does not parse: %v\nconfig: %+v\noutput: %q", err, cfg, out)
	}
	if !Equal(v, back) {
		t.Fatalf("round trip changed the value\nconfig: %+v\noutput: %q", cfg, out)
	}
}

func TestPrettyRoundTripSeeds(t *testing.T) {
	for _, s := range fuzzSeeds {
		v, err := Parse([]byte(s))
		if err != nil {
			continue
		}
		for b := range uint16(1024) {
			if b%7 != 0 && b > 63 {
				continue
			}
			checkPretty(t, v, prettyConfigFromBits(b, uint8(b)))
		}
	}
}

// FuzzPretty checks that output laid out by any PrettyConfig parses back to
// the value that was printed.
func FuzzPretty(f *testing.F) {
	for i, s := range fuzzSeeds {
		f.Add([]byte(s), uint16(i*37), uint8(i))
	}
	f.Add([]byte(prettyDoc), uint16(0x3ff), uint8(2))
	f.Fuzz(func(t *testing.T, data []byte, bits uint16, depth uint8) {
		v, err := Parse(data)
		if err != nil {
			return
		}
		checkPretty(t, v, prettyConfigFromBits(bits, depth))
	})
}
