// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

//go:build js && wasm

// Command wasm exposes rustyon to the browser playground through syscall/js.
//
// Every exported function takes (input string, indent string) and returns an
// object {ok, output, error, line, column}. line and column are 0 when the
// error has no known position.
package main

import (
	"encoding/json"
	"errors"
	"syscall/js"
	"unicode/utf8"

	"github.com/peltonapp/rustyon-go"
	"github.com/peltonapp/rustyon-go/ronjson"
)

func main() {
	js.Global().Set("rustyonFormat", js.FuncOf(wrap(format)))
	js.Global().Set("rustyonValidate", js.FuncOf(wrap(validate)))
	js.Global().Set("rustyonToJSON", js.FuncOf(wrap(toJSON)))
	js.Global().Set("rustyonFromJSON", js.FuncOf(wrap(fromJSON)))
	js.Global().Set("rustyonReady", true)
	select {}
}

type result struct {
	output string
	err    error
}

func wrap(fn func(src []byte, indent string) result) func(js.Value, []js.Value) any {
	return func(_ js.Value, args []js.Value) any {
		var src, indent string
		if len(args) > 0 {
			src = args[0].String()
		}
		if len(args) > 1 {
			indent = args[1].String()
		}
		r := fn([]byte(src), indent)
		out := map[string]any{"ok": r.err == nil, "output": r.output, "error": "", "line": 0, "column": 0}
		if r.err != nil {
			out["error"] = r.err.Error()
			line, col := position(r.err, src)
			out["line"], out["column"] = line, col
		}
		return js.ValueOf(out)
	}
}

func format(src []byte, indent string) result {
	out, err := rustyon.FormatIndent(src, indent)
	return result{string(out), err}
}

func validate(src []byte, _ string) result {
	if _, err := rustyon.Parse(src); err != nil {
		return result{err: err}
	}
	return result{output: "Valid RON"}
}

func toJSON(src []byte, indent string) result {
	out, err := ronjson.ToJSON(src, indent)
	return result{string(out), err}
}

func fromJSON(src []byte, indent string) result {
	out, err := ronjson.FromJSON(src, ronjson.Options{Structs: true, Indent: indent})
	return result{string(out), err}
}

// position extracts a 1-based line and column from a RON or JSON syntax error.
func position(err error, src string) (int, int) {
	var se *rustyon.SyntaxError
	if errors.As(err, &se) {
		return se.Pos.Line, se.Pos.Column
	}
	var je *json.SyntaxError
	if errors.As(err, &je) {
		return offsetToLineCol(src, int(je.Offset))
	}
	return 0, 0
}

// offsetToLineCol converts a byte offset (the number of bytes consumed) into
// a 1-based line and column counted in code points.
func offsetToLineCol(src string, off int) (int, int) {
	if off > len(src) {
		off = len(src)
	}
	if off < 1 {
		return 1, 1
	}
	// encoding/json reports the offset just past the offending byte.
	off--
	line, col := 1, 1
	for i := 0; i < off; {
		r, n := utf8.DecodeRuneInString(src[i:])
		i += n
		if r == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}
