// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/peltonapp/rustyon-go"
)

// maxSnippetWidth is the most source characters shown for one line. Longer
// lines are trimmed around the error column.
const maxSnippetWidth = 80

// contextBefore is how many characters before the error column stay visible
// when a long line is trimmed.
const contextBefore = 30

// reportError prints err for the input in to w. A *rustyon.SyntaxError is
// shown as "name:line:col: message" followed by the source line and a caret
// under the offending column; other errors are shown as "name: error".
func reportError(w io.Writer, in input, err error) {
	var se *rustyon.SyntaxError
	if !errors.As(err, &se) {
		fmt.Fprintf(w, "%s: %v\n", in.name, err)
		return
	}
	fmt.Fprintf(w, "%s:%d:%d: %s\n", in.name, se.Pos.Line, se.Pos.Column, se.Msg)
	fmt.Fprint(w, snippet(in.data, se.Pos))
}

// snippet renders the source line of pos with a caret line below it. It
// returns an empty string when the position does not point into data.
//
// Position.Column counts code points, so the caret is placed by rune, not by
// byte. Tabs before the caret are repeated in the caret line so it lines up
// at any tab width.
func snippet(data []byte, pos rustyon.Position) string {
	if pos.Line < 1 {
		return ""
	}
	lines := bytes.Split(data, []byte("\n"))
	if pos.Line > len(lines) {
		return ""
	}
	runes := []rune(strings.TrimSuffix(string(lines[pos.Line-1]), "\r"))
	col := pos.Column - 1
	if col < 0 {
		col = 0
	}
	if col > len(runes) {
		col = len(runes)
	}

	start, end := 0, len(runes)
	if len(runes) > maxSnippetWidth {
		start = max(0, col-contextBefore)
		end = min(len(runes), start+maxSnippetWidth)
		start = max(0, end-maxSnippetWidth)
		if col >= end {
			// The caret may sit one past the visible text (end of line).
			end = min(len(runes), col+1)
			start = max(0, end-maxSnippetWidth)
		}
	}

	prefix, suffix := "", ""
	if start > 0 {
		prefix = "..."
	}
	if end < len(runes) {
		suffix = "..."
	}

	var text, caret strings.Builder
	text.WriteString(prefix)
	caret.WriteString(strings.Repeat(" ", len(prefix)))
	for i := start; i < end; i++ {
		text.WriteRune(runes[i])
		if i < col {
			if runes[i] == '\t' {
				caret.WriteByte('\t')
			} else {
				caret.WriteByte(' ')
			}
		}
	}
	text.WriteString(suffix)
	caret.WriteByte('^')

	num := strconv.Itoa(pos.Line)
	gutter := strings.Repeat(" ", len(num))
	return fmt.Sprintf(" %s | %s\n %s | %s\n", num, text.String(), gutter, caret.String())
}
