// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"strings"
	"testing"
)

func TestToYAML(t *testing.T) {
	src := `(name: "yes", items: [(a: 1, b: Some(2.5)), (a: 3, b: None)], m: {"k": 1}, big: 340282366920938463463374607431768211455)`
	want := `name: "yes"
items:
  - a: 1
    b: 2.5
  - a: 3
    b: null
m:
  k: 1
big: 340282366920938463463374607431768211455
`
	code, out, errOut := runCLI(t, src, "to-yaml")
	if code != 0 || out != want {
		t.Errorf("code %d, stderr %q, out:\n%s\nwant:\n%s", code, errOut, out, want)
	}
	code, out, _ = runCLI(t, "[1]", "to-yaml", "-indent", "4")
	if code != 0 || out != "- 1\n" {
		t.Errorf("indent: code %d, out %q", code, out)
	}
	if code, _, _ := runCLI(t, "[1]", "to-yaml", "-indent", "0"); code != 2 {
		t.Errorf("indent 0: code %d", code)
	}
	if code, _, errOut := runCLI(t, "NaN", "to-yaml"); code != 1 || !strings.Contains(errOut, "cannot be represented") {
		t.Errorf("NaN: code %d, stderr %q", code, errOut)
	}
	// Strings that look like other scalars stay strings.
	code, out, _ = runCLI(t, `["true", "12", "no", "plain"]`, "to-yaml")
	if code != 0 || out != "- \"true\"\n- \"12\"\n- \"no\"\n- plain\n" {
		t.Errorf("quoting: code %d, out %q", code, out)
	}
}

func TestFromYAML(t *testing.T) {
	src := "name: demo\nports: [80, 0x1f, 1_000]\nratio: 1.5\nbig: 340282366920938463463374607431768211455\non: ~\nflag: true\nbase: &b {x: 1}\ncopy: *b\n"
	want := `(
    name: "demo",
    ports: [
        80,
        31,
        1000,
    ],
    ratio: 1.5,
    big: 340282366920938463463374607431768211455,
    on: None,
    flag: true,
    base: (
        x: 1,
    ),
    copy: (
        x: 1,
    ),
)
`
	code, out, errOut := runCLI(t, src, "from-yaml", "-structs")
	if code != 0 || out != want {
		t.Errorf("code %d, stderr %q, out:\n%s\nwant:\n%s", code, errOut, out, want)
	}
	code, out, _ = runCLI(t, "a: 1\nb: [x]\n", "from-yaml", "-indent", "")
	if code != 0 || out != "{\"a\":1,\"b\":[\"x\"]}\n" {
		t.Errorf("compact map: code %d, out %q", code, out)
	}
	for _, bad := range []string{"a: [", "", "---\na: 1\n---\nb: 2\n", "? [a]\n: 1\n", "x: .nan\n"} {
		if code, _, errOut := runCLI(t, bad, "from-yaml"); code != 1 || errOut == "" {
			t.Errorf("%q: code %d, stderr %q", bad, code, errOut)
		}
	}
}

func TestToTOML(t *testing.T) {
	src := `(title: "x", port: 80, ratio: 1.5, off: None, db: (host: "h", ports: [1, 2]), servers: [(name: "a"), (name: "b")])`
	code, out, errOut := runCLI(t, src, "to-toml")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"title = 'x'", "port = 80", "ratio = 1.5", "[db]", "host = 'h'", "ports = [1, 2]", "[[servers]]", "name = 'a'"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "off") {
		t.Errorf("None should be left out:\n%s", out)
	}
	for name, bad := range map[string]string{
		"top level list": "[1]",
		"null in array":  "(a: [1, None])",
		"huge integer":   "(a: 340282366920938463463374607431768211455)",
	} {
		if code, _, errOut := runCLI(t, bad, "to-toml"); code != 1 || errOut == "" {
			t.Errorf("%s: code %d, stderr %q", name, code, errOut)
		}
	}
}

func TestFromTOML(t *testing.T) {
	src := "title = \"x\"\nwhen = 1979-05-27\n\n[server]\nport = 80\n\n[[items]]\nn = 1\n"
	want := `(
    items: [
        (
            n: 1,
        ),
    ],
    server: (
        port: 80,
    ),
    title: "x",
    when: "1979-05-27",
)
`
	code, out, errOut := runCLI(t, src, "from-toml", "-structs")
	if code != 0 || out != want {
		t.Errorf("code %d, stderr %q, out:\n%s\nwant:\n%s", code, errOut, out, want)
	}
	if code, _, errOut := runCLI(t, "a = ", "from-toml"); code != 1 || errOut == "" {
		t.Errorf("invalid: code %d, stderr %q", code, errOut)
	}
}

func TestConvertRoundTrip(t *testing.T) {
	src := `(a: [1, 2, 3], b: (c: "d"))`
	_, yml, _ := runCLI(t, src, "to-yaml")
	code, out, _ := runCLI(t, yml, "from-yaml", "-structs", "-indent", "")
	if code != 0 || out != "(a:[1,2,3],b:(c:\"d\"))\n" {
		t.Errorf("yaml: code %d, out %q", code, out)
	}
	_, tml, _ := runCLI(t, src, "to-toml")
	code, out, _ = runCLI(t, tml, "from-toml", "-structs", "-indent", "")
	if code != 0 || out != "(a:[1,2,3],b:(c:\"d\"))\n" {
		t.Errorf("toml: code %d, out %q", code, out)
	}
}
