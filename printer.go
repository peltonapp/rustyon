// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/peltonapp/rustyon-go/internal/syntax"
)

// printer renders a Value tree as RON text.
type printer struct {
	sb     strings.Builder
	prefix string
	indent string // empty means compact output
	level  int
	err    error
}

func (p *printer) pretty() bool { return p.indent != "" }

func (p *printer) newline() {
	p.sb.WriteByte('\n')
	p.sb.WriteString(p.prefix)
	for range p.level {
		p.sb.WriteString(p.indent)
	}
}

// ident writes an identifier, recording an error when name cannot be
// represented even as a raw identifier.
func (p *printer) ident(name string) {
	if !isRawIdent(name) {
		if p.err == nil {
			p.err = &UnsupportedValueError{Msg: "invalid identifier " + strconv.Quote(name)}
		}
		return
	}
	p.sb.WriteString(formatIdent(name))
}

func (p *printer) writeHeader(exts Extensions) {
	if exts == 0 {
		return
	}
	p.sb.WriteString("#![enable(")
	sep := ","
	if p.pretty() {
		sep = ", "
	}
	p.sb.WriteString(strings.Join(exts.Names(), sep))
	p.sb.WriteString(")]")
	if p.pretty() {
		p.sb.WriteByte('\n')
		p.sb.WriteString(p.prefix)
	}
}

func (p *printer) value(v Value) {
	switch v := v.(type) {
	case nil:
		p.sb.WriteString("None")
	case Unit:
		p.sb.WriteString("()")
	case Bool:
		if v {
			p.sb.WriteString("true")
		} else {
			p.sb.WriteString("false")
		}
	case Int:
		p.sb.WriteString(v.String())
	case Float:
		p.sb.WriteString(formatFloat(float64(v), 64))
	case String:
		p.sb.WriteString(quoteString(string(v)))
	case Char:
		p.sb.WriteString(quoteChar(rune(v)))
	case Bytes:
		p.sb.WriteString(quoteBytes(v))
	case Ident:
		p.ident(string(v))
	case Option:
		if v.Value == nil {
			p.sb.WriteString("None")
			return
		}
		p.sb.WriteString("Some(")
		p.value(v.Value)
		p.sb.WriteByte(')')
	case List:
		p.seq('[', ']', len(v), true, func(i int) { p.value(v[i]) })
	case Map:
		p.seq('{', '}', len(v), true, func(i int) {
			p.value(v[i].Key)
			p.colon()
			p.value(v[i].Value)
		})
	case Tuple:
		if v.Name != "" {
			p.ident(v.Name)
		}
		p.seq('(', ')', len(v.Values), !allScalar(v.Values), func(i int) { p.value(v.Values[i]) })
	case Struct:
		if v.Name != "" {
			p.ident(v.Name)
		}
		if len(v.Fields) == 0 && v.Name != "" {
			// A named struct without fields is a unit struct.
			return
		}
		p.seq('(', ')', len(v.Fields), true, func(i int) {
			p.ident(v.Fields[i].Name)
			p.colon()
			p.value(v.Fields[i].Value)
		})
	}
}

func (p *printer) colon() {
	if p.pretty() {
		p.sb.WriteString(": ")
	} else {
		p.sb.WriteByte(':')
	}
}

// seq prints a delimited sequence. When expand is true and the printer is in
// pretty mode, every element goes on its own line with a trailing comma.
func (p *printer) seq(open, close byte, n int, expand bool, elem func(int)) {
	p.sb.WriteByte(open)
	if n == 0 {
		p.sb.WriteByte(close)
		return
	}
	if p.pretty() && expand {
		p.level++
		for i := range n {
			p.newline()
			elem(i)
			p.sb.WriteByte(',')
		}
		p.level--
		p.newline()
	} else {
		for i := range n {
			if i > 0 {
				p.sb.WriteByte(',')
				if p.pretty() {
					p.sb.WriteByte(' ')
				}
			}
			elem(i)
		}
	}
	p.sb.WriteByte(close)
}

func allScalar(vs []Value) bool {
	for _, v := range vs {
		switch v := v.(type) {
		case List, Map, Struct:
			return false
		case Tuple:
			if !allScalar(v.Values) {
				return false
			}
		case Option:
			if v.Value != nil && !allScalar([]Value{v.Value}) {
				return false
			}
		}
	}
	return true
}

func formatFloat(f float64, bits int) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	}
	s := strconv.FormatFloat(f, 'g', -1, bits)
	if !strings.ContainsAny(s, ".eEn") {
		s += ".0"
	}
	return s
}

func quoteString(s string) string {
	var sb strings.Builder
	sb.Grow(len(s) + 2)
	sb.WriteByte('"')
	for _, r := range s {
		writeEscaped(&sb, r, '"')
	}
	sb.WriteByte('"')
	return sb.String()
}

func quoteChar(r rune) string {
	var sb strings.Builder
	sb.WriteByte('\'')
	writeEscaped(&sb, r, '\'')
	sb.WriteByte('\'')
	return sb.String()
}

func writeEscaped(sb *strings.Builder, r rune, quote rune) {
	switch r {
	case quote, '\\':
		sb.WriteByte('\\')
		sb.WriteRune(r)
	case '\n':
		sb.WriteString(`\n`)
	case '\r':
		sb.WriteString(`\r`)
	case '\t':
		sb.WriteString(`\t`)
	case 0:
		sb.WriteString(`\0`)
	default:
		if r == utf8.RuneError || !unicode.IsPrint(r) {
			sb.WriteString(`\u{`)
			sb.WriteString(strconv.FormatInt(int64(r), 16))
			sb.WriteByte('}')
			return
		}
		sb.WriteRune(r)
	}
}

func quoteBytes(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b) + 3)
	sb.WriteString(`b"`)
	const hex = "0123456789abcdef"
	for _, c := range b {
		switch {
		case c == '"' || c == '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		case c == '\n':
			sb.WriteString(`\n`)
		case c == '\r':
			sb.WriteString(`\r`)
		case c == '\t':
			sb.WriteString(`\t`)
		case c >= 0x20 && c < 0x7F:
			sb.WriteByte(c)
		default:
			sb.WriteString(`\x`)
			sb.WriteByte(hex[c>>4])
			sb.WriteByte(hex[c&0xF])
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

var reservedIdents = map[string]bool{
	"true": true, "false": true, "inf": true, "NaN": true, "None": true, "Some": true,
}

// formatIdent writes name as an identifier, using the raw form r#name when
// the name is reserved or contains characters not allowed in a plain
// identifier.
func formatIdent(name string) string {
	if isPlainIdent(name) && !reservedIdents[name] {
		return name
	}
	return "r#" + name
}

func isPlainIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 && !syntax.IsIdentStart(r) {
			return false
		}
		if !syntax.IsIdentContinue(r) {
			return false
		}
	}
	return true
}

// isRawIdent reports whether s can be written as a raw identifier.
func isRawIdent(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !syntax.IsRawIdentContinue(r) {
			return false
		}
	}
	return true
}
