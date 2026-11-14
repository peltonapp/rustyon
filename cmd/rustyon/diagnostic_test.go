// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/peltonapp/rustyon-go"
)

func TestSnippet(t *testing.T) {
	long := strings.Repeat("a", 200)
	tests := []struct {
		name string
		src  string
		pos  rustyon.Position
		want string
	}{
		{
			name: "simple",
			src:  "(a: 1 2)",
			pos:  rustyon.Position{Line: 1, Column: 7},
			want: " 1 | (a: 1 2)\n   |       ^\n",
		},
		{
			name: "second line and wide gutter",
			src:  strings.Repeat("\n", 8) + "  x y\n",
			pos:  rustyon.Position{Line: 9, Column: 5},
			want: " 9 |   x y\n   |     ^\n",
		},
		{
			name: "tabs are kept in the caret line",
			src:  "\t\t(x)",
			pos:  rustyon.Position{Line: 1, Column: 4},
			want: " 1 | \t\t(x)\n   | \t\t ^\n",
		},
		{
			name: "columns count runes, not bytes",
			src:  "(\"h\u00e9llo\" x)",
			pos:  rustyon.Position{Line: 1, Column: 10},
			want: " 1 | (\"h\u00e9llo\" x)\n   |          ^\n",
		},
		{
			name: "carriage return is dropped",
			src:  "(a\r\nb)",
			pos:  rustyon.Position{Line: 1, Column: 3},
			want: " 1 | (a\n   |   ^\n",
		},
		{
			name: "end of line",
			src:  "(a:",
			pos:  rustyon.Position{Line: 1, Column: 4},
			want: " 1 | (a:\n   |    ^\n",
		},
		{
			name: "long line, error in the middle",
			src:  long + "X" + long,
			pos:  rustyon.Position{Line: 1, Column: 201},
			want: " 1 | ..." + strings.Repeat("a", 30) + "X" + strings.Repeat("a", 49) + "...\n" +
				"   | " + strings.Repeat(" ", 33) + "^\n",
		},
		{
			name: "long line, error at the start",
			src:  "X" + long,
			pos:  rustyon.Position{Line: 1, Column: 1},
			want: " 1 | X" + strings.Repeat("a", 79) + "...\n   | ^\n",
		},
		{
			name: "long line, error at the end",
			src:  long + "(",
			pos:  rustyon.Position{Line: 1, Column: 202},
			want: " 1 | ..." + strings.Repeat("a", 79) + "(\n   | " + strings.Repeat(" ", 3+80) + "^\n",
		},
		{name: "line out of range", src: "a", pos: rustyon.Position{Line: 3, Column: 1}, want: ""},
		{name: "no line", src: "a", pos: rustyon.Position{}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := snippet([]byte(tt.src), tt.pos); got != tt.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestReportError(t *testing.T) {
	_, err := rustyon.Parse([]byte("(a: 1 2)"))
	var sb strings.Builder
	reportError(&sb, input{name: "x.ron", data: []byte("(a: 1 2)")}, err)
	got := sb.String()
	if !strings.HasPrefix(got, "x.ron:1:") || !strings.Contains(got, "(a: 1 2)") || !strings.HasSuffix(got, "^\n") {
		t.Errorf("unexpected report:\n%s", got)
	}

	sb.Reset()
	reportError(&sb, input{name: "f"}, errReported)
	if sb.String() != "f: errors were reported\n" {
		t.Errorf("non syntax error: %q", sb.String())
	}
}

// TestCaretPointsAtError checks the caret against real parser positions,
// including multi byte text and tabs before the error.
func TestCaretPointsAtError(t *testing.T) {
	for _, src := range []string{
		"(a: 1 2)",
		"\t(a: [1, 2 3])",
		"(\"h\u00e9llo w\u00f6rld\": [1,, 2])",
		"(\n  a: 1,\n  b: @,\n)",
	} {
		_, err := rustyon.Parse([]byte(src))
		if err == nil {
			t.Fatalf("%q parsed", src)
		}
		code, _, errOut := runCLI(t, src, "validate")
		if code != 1 {
			t.Fatalf("%q: code %d", src, code)
		}
		lines := strings.Split(strings.TrimRight(errOut, "\n"), "\n")
		if len(lines) != 3 {
			t.Fatalf("%q: want 3 lines, got %q", src, errOut)
		}
		_, srcLine, _ := strings.Cut(lines[1], " | ")
		_, caretLine, _ := strings.Cut(lines[2], " | ")
		col := len([]rune(caretLine)) // caret is the last rune
		if r := []rune(srcLine); col > len(r)+1 {
			t.Errorf("%q: caret column %d is beyond the line %q", src, col, srcLine)
		}
		var se *rustyon.SyntaxError
		if !errors.As(err, &se) || se.Pos.Column != col {
			t.Errorf("%q: caret at column %d, error says %v", src, col, err)
		}
	}
}

func TestCommandsShowSnippets(t *testing.T) {
	for _, args := range [][]string{{"fmt"}, {"validate"}, {"to-json"}, {"to-yaml"}, {"to-toml"}, {"get", "."}} {
		code, _, errOut := runCLI(t, "(a: 1 2)", args...)
		if code != 1 || !strings.HasPrefix(errOut, "<stdin>:1:") || !strings.Contains(errOut, "^") {
			t.Errorf("%v: code %d, stderr %q", args, code, errOut)
		}
	}
}
