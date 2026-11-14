// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(t *testing.T, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestUsage(t *testing.T) {
	if code, _, errOut := runCLI(t, ""); code != 2 || !strings.Contains(errOut, "Usage") {
		t.Errorf("no args: code %d, stderr %q", code, errOut)
	}
	if code, out, _ := runCLI(t, "", "help"); code != 0 || !strings.Contains(out, "Commands") {
		t.Errorf("help: code %d, stdout %q", code, out)
	}
	if code, _, errOut := runCLI(t, "", "bogus"); code != 2 || !strings.Contains(errOut, "unknown command") {
		t.Errorf("bogus: code %d, stderr %q", code, errOut)
	}
	if code, out, _ := runCLI(t, "", "version"); code != 0 || !strings.HasPrefix(out, "rustyon ") {
		t.Errorf("version: code %d, stdout %q", code, out)
	}
	if code, _, _ := runCLI(t, "", "fmt", "-h"); code != 0 {
		t.Errorf("fmt -h: code %d", code)
	}
	if code, _, _ := runCLI(t, "", "fmt", "-nope"); code != 2 {
		t.Errorf("bad flag: code %d", code)
	}
}

func TestFmtStdin(t *testing.T) {
	code, out, _ := runCLI(t, "(a:1, // c\nb:[1,2])", "fmt")
	want := "(\n    a: 1, // c\n    b: [1, 2],\n)\n"
	if code != 0 || out != want {
		t.Errorf("code %d, out %q, want %q", code, out, want)
	}
	code, _, errOut := runCLI(t, "(a:", "fmt")
	if code != 1 || !strings.Contains(errOut, "<stdin>") {
		t.Errorf("invalid input: code %d, stderr %q", code, errOut)
	}
	if code, _, _ := runCLI(t, "1", "fmt", "-w"); code != 2 {
		t.Errorf("-w without files: code %d", code)
	}
}

func TestFmtFiles(t *testing.T) {
	dir := t.TempDir()
	messy := filepath.Join(dir, "messy.ron")
	clean := filepath.Join(dir, "clean.ron")
	if err := os.WriteFile(messy, []byte("[1,\n2]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(clean, []byte("[1, 2]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runCLI(t, "", "fmt", "-l", messy, clean)
	if code != 0 || out != messy+"\n" {
		t.Errorf("-l: code %d, out %q", code, out)
	}
	if code, _, _ := runCLI(t, "", "fmt", "-w", messy); code != 0 {
		t.Fatalf("-w: code %d", code)
	}
	got, _ := os.ReadFile(messy)
	if string(got) != "[\n    1,\n    2,\n]\n" {
		t.Errorf("-w wrote %q", got)
	}
	if code, _, _ := runCLI(t, "", "fmt", filepath.Join(dir, "missing.ron")); code != 1 {
		t.Errorf("missing file: code %d", code)
	}
}

func TestValidate(t *testing.T) {
	if code, out, _ := runCLI(t, "(a: 1)", "validate"); code != 0 || out != "<stdin>: ok\n" {
		t.Errorf("valid: code %d, out %q", code, out)
	}
	if code, out, _ := runCLI(t, "(a: 1)", "validate", "-q"); code != 0 || out != "" {
		t.Errorf("quiet: code %d, out %q", code, out)
	}
	code, _, errOut := runCLI(t, "(a: ", "validate")
	if code != 1 || !strings.Contains(errOut, "syntax error") {
		t.Errorf("invalid: code %d, stderr %q", code, errOut)
	}
}

func TestToJSON(t *testing.T) {
	code, out, _ := runCLI(t, "(a: [1, Some(2)])", "to-json", "-indent", "")
	if code != 0 || out != "{\"a\":[1,2]}\n" {
		t.Errorf("code %d, out %q", code, out)
	}
	if code, _, errOut := runCLI(t, "NaN", "to-json"); code != 1 || !strings.Contains(errOut, "cannot be represented") {
		t.Errorf("NaN: code %d, stderr %q", code, errOut)
	}
	if code, _, _ := runCLI(t, "", "to-json", "a", "b"); code != 2 {
		t.Errorf("two files: code %d", code)
	}
}

func TestFromJSON(t *testing.T) {
	code, out, _ := runCLI(t, `{"a": [1, null]}`, "from-json", "-structs")
	want := "(\n    a: [\n        1,\n        None,\n    ],\n)\n"
	if code != 0 || out != want {
		t.Errorf("code %d, out %q, want %q", code, out, want)
	}
	if code, _, _ := runCLI(t, `{`, "from-json"); code != 1 {
		t.Errorf("invalid json: code %d", code)
	}
}
