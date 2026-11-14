// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"bytes"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/peltonapp/rustyon-go/internal/syntax"
)

// printer renders a Value tree as RON text.
type printer struct {
	sb      bytes.Buffer
	prefix  string
	indent  string // empty means compact output
	level   int
	err     error
	scratch [32]byte

	// Layout options, see PrettyConfig. The zero values give the default
	// layout of MarshalIndent.
	nl             string // line break, "\n" when empty
	colonSep       string // after a colon in pretty mode, ": " when empty
	depthLimit     int    // collections nested deeper than this are inline, 0 for no limit
	compactArrays  bool
	compactStructs bool
	compactMaps    bool
	separateTuples bool
	enumerate      bool
	escapeNonASCII bool

	depth  int // nesting depth of the collection being written
	inline int // greater than zero while writing a subtree on one line
}

func (p *printer) pretty() bool { return p.indent != "" }

// expanded reports whether collections may be spread over several lines.
func (p *printer) expanded() bool { return p.indent != "" && p.inline == 0 }

// configure applies the layout of a PrettyConfig.
func (p *printer) configure(c *PrettyConfig) {
	p.prefix = c.Prefix
	p.indent = c.Indent
	if p.indent == "" {
		p.indent = "    "
	}
	p.nl = c.NewLine
	if c.Separator != "" {
		p.colonSep = ":" + c.Separator
	}
	p.depthLimit = c.DepthLimit
	p.compactArrays = c.CompactArrays
	p.compactStructs = c.CompactStructs
	p.compactMaps = c.CompactMaps
	p.separateTuples = c.SeparateTupleMembers
	p.enumerate = c.EnumerateArrays
	p.escapeNonASCII = c.EscapeNonASCII
}

func (p *printer) str(s string) { writeQuotedEsc(&p.sb, s, p.escapeNonASCII) }

func (p *printer) newline() {
	if p.nl == "" {
		p.sb.WriteByte('\n')
	} else {
		p.sb.WriteString(p.nl)
	}
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
		p.newline()
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
		if v.b == nil {
			p.sb.Write(strconv.AppendInt(p.scratch[:0], v.small, 10))
		} else {
			p.sb.WriteString(v.b.String())
		}
	case Float:
		p.sb.WriteString(formatFloat(float64(v), 64))
	case String:
		p.str(string(v))
	case Char:
		p.sb.WriteString(quoteCharEsc(rune(v), p.escapeNonASCII))
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
		p.seq('[', ']', len(v), true, p.compactArrays, p.enumerate, func(i int) { p.value(v[i]) })
	case Map:
		p.seq('{', '}', len(v), true, p.compactMaps, false, func(i int) {
			p.value(v[i].Key)
			p.colon()
			p.value(v[i].Value)
		})
	case Tuple:
		if v.Name != "" {
			p.ident(v.Name)
		} else if len(v.Values) == 0 {
			// "()" is the unit value, an empty tuple needs the space.
			p.sb.WriteString("( )")
			return
		}
		p.seq('(', ')', len(v.Values), p.separateTuples || !allScalar(v.Values), false, false, func(i int) { p.value(v.Values[i]) })
	case Struct:
		if v.Name != "" {
			p.ident(v.Name)
		}
		if len(v.Fields) == 0 && v.Name != "" {
			// A named struct without fields is a unit struct.
			return
		}
		p.seq('(', ')', len(v.Fields), true, p.compactStructs, false, func(i int) {
			p.ident(v.Fields[i].Name)
			p.colon()
			p.value(v.Fields[i].Value)
		})
	}
}

func (p *printer) colon() {
	switch {
	case !p.pretty():
		p.sb.WriteByte(':')
	case p.colonSep != "":
		p.sb.WriteString(p.colonSep)
	default:
		p.sb.WriteString(": ")
	}
}

// seq prints a delimited sequence. When expand is true and the printer is in
// pretty mode, every element goes on its own line with a trailing comma. A
// compact sequence, and every sequence below the depth limit, is written on
// one line together with everything it contains. With enumerate, expanded
// elements are preceded by a comment with their index.
func (p *printer) seq(open, close byte, n int, expand, compact, enumerate bool, elem func(int)) {
	p.sb.WriteByte(open)
	if n == 0 {
		p.sb.WriteByte(close)
		return
	}
	p.depth++
	forceInline := compact || (p.depthLimit > 0 && p.depth > p.depthLimit)
	if forceInline {
		p.inline++
	}
	if p.expanded() && expand {
		p.level++
		for i := range n {
			p.newline()
			if enumerate {
				p.sb.WriteString("// [")
				p.sb.Write(strconv.AppendInt(p.scratch[:0], int64(i), 10))
				p.sb.WriteByte(']')
				p.newline()
			}
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
	if forceInline {
		p.inline--
	}
	p.depth--
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

// writeQuoted writes s as a double quoted RON string.
func writeQuoted(sb *bytes.Buffer, s string) { writeQuotedEsc(sb, s, false) }

// writeQuotedEsc is like writeQuoted and escapes all non-ASCII characters as
// \u{...} when escapeNonASCII is set.
func writeQuotedEsc(sb *bytes.Buffer, s string, escapeNonASCII bool) {
	sb.WriteByte('"')
	// Fast path: printable ASCII without quotes or backslashes needs no
	// escaping, which is the common case for keys and names.
	plain := true
	for i := range len(s) {
		if c := s[i]; c < 0x20 || c >= 0x7F || c == '"' || c == '\\' {
			plain = false
			break
		}
	}
	if plain {
		sb.WriteString(s)
	} else {
		for _, r := range s {
			writeEscaped(sb, r, '"', escapeNonASCII)
		}
	}
	sb.WriteByte('"')
}

func quoteCharEsc(r rune, escapeNonASCII bool) string {
	var sb bytes.Buffer
	sb.WriteByte('\'')
	writeEscaped(&sb, r, '\'', escapeNonASCII)
	sb.WriteByte('\'')
	return sb.String()
}

func writeEscaped(sb *bytes.Buffer, r rune, quote rune, escapeNonASCII bool) {
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
		if r == utf8.RuneError || !unicode.IsPrint(r) || (escapeNonASCII && r >= 0x80) {
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
	"true": true, "false": true, "None": true, "Some": true,
	"inf": true, "inff32": true, "inff64": true, "NaN": true, "NaNf32": true, "NaNf64": true,
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
