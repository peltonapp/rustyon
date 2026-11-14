// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
)

// maxDepth limits nesting to protect against stack exhaustion on hostile
// input.
const maxDepth = 1000

// Extensions is a set of RON extensions enabled through an
// #![enable(...)] attribute at the start of a document.
type Extensions uint8

// RON extensions as defined by ron-rs.
const (
	// ImplicitSome allows Some(x) to be written as x.
	ImplicitSome Extensions = 1 << iota
	// UnwrapNewtypes allows newtype structs such as Meters(5.0) to be
	// written as their inner value 5.0.
	UnwrapNewtypes
	// UnwrapVariantNewtypes allows newtype variants that wrap a struct to
	// omit the inner parentheses: Shape(x: 1) instead of Shape((x: 1)).
	UnwrapVariantNewtypes
	// ExplicitStructNames requires struct names to be written.
	ExplicitStructNames
)

var extensionNames = []struct {
	ext  Extensions
	name string
}{
	{ImplicitSome, "implicit_some"},
	{UnwrapNewtypes, "unwrap_newtypes"},
	{UnwrapVariantNewtypes, "unwrap_variant_newtypes"},
	{ExplicitStructNames, "explicit_struct_names"},
}

// Names returns the RON names of the enabled extensions.
func (e Extensions) Names() []string {
	var names []string
	for _, x := range extensionNames {
		if e&x.ext != 0 {
			names = append(names, x.name)
		}
	}
	return names
}

func extensionByName(name string) (Extensions, bool) {
	for _, x := range extensionNames {
		if x.name == name {
			return x.ext, true
		}
	}
	return 0, false
}

// Document is a parsed RON document: an optional set of extensions and a
// single value.
type Document struct {
	Extensions Extensions
	Value      Value
}

// Parse parses a RON document into a [Value]. Extension attributes are
// accepted but not returned; use [ParseDocument] to inspect them.
func Parse(data []byte) (Value, error) {
	doc, err := ParseDocument(data)
	if err != nil {
		return nil, err
	}
	return doc.Value, nil
}

// ParseDocument parses a RON document including its extension attributes.
func ParseDocument(data []byte) (*Document, error) {
	p, err := newParser(data)
	if err != nil {
		return nil, err
	}
	doc, err := p.parseDocument()
	if err != nil {
		return nil, err
	}
	if p.tok.kind != tokEOF {
		return nil, p.unexpected("end of input")
	}
	return doc, nil
}

// Valid reports whether data is a syntactically valid RON document.
func Valid(data []byte) bool {
	_, err := ParseDocument(data)
	return err == nil
}

type parser struct {
	lex   *lexer
	tok   token   // current token, never a comment
	ahead []token // lookahead buffer, never comments
	depth int
}

func newParser(data []byte) (*parser, error) {
	if !utf8.Valid(data) {
		l := newLexer(data)
		for l.off < len(l.src) {
			if r, size := utf8.DecodeRuneInString(l.src[l.off:]); r == utf8.RuneError && size == 1 {
				break
			}
			l.advance()
		}
		return nil, &SyntaxError{Msg: "invalid UTF-8 encoding", Pos: l.pos()}
	}
	p := &parser{lex: newLexer(data)}
	if err := p.next(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *parser) lexNonComment() (token, error) {
	for {
		t, err := p.lex.next()
		if err != nil {
			return t, err
		}
		if t.kind != tokLineComment && t.kind != tokBlockComment {
			return t, nil
		}
	}
}

func (p *parser) next() error {
	if len(p.ahead) > 0 {
		p.tok = p.ahead[0]
		p.ahead = p.ahead[1:]
		return nil
	}
	t, err := p.lexNonComment()
	if err != nil {
		return err
	}
	p.tok = t
	return nil
}

// peek returns the token after the current one.
func (p *parser) peek() (token, error) {
	if len(p.ahead) == 0 {
		t, err := p.lexNonComment()
		if err != nil {
			return t, err
		}
		p.ahead = append(p.ahead, t)
	}
	return p.ahead[0], nil
}

func (p *parser) errorf(format string, args ...any) error {
	return p.lex.errorf(p.tok.pos, format, args...)
}

func describe(t token) string {
	switch t.kind {
	case tokEOF:
		return "end of input"
	case tokIdent:
		return "identifier " + t.raw
	case tokInt, tokFloat, tokString, tokByteString, tokChar, tokByte:
		raw := t.raw
		if len(raw) > 32 {
			raw = raw[:29] + "..."
		}
		return t.kind.String() + " " + raw
	}
	return t.kind.String()
}

func (p *parser) unexpected(want string) error {
	return p.errorf("expected %s, found %s", want, describe(p.tok))
}

func (p *parser) expect(k tokenKind) error {
	if p.tok.kind != k {
		return p.unexpected(k.String())
	}
	return p.next()
}

func (p *parser) parseDocument() (*Document, error) {
	doc := &Document{}
	for p.tok.kind == tokHash {
		ext, err := p.parseAttribute()
		if err != nil {
			return nil, err
		}
		doc.Extensions |= ext
	}
	v, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	doc.Value = v
	return doc, nil
}

// parseAttribute parses #![enable(a, b, ...)].
func (p *parser) parseAttribute() (Extensions, error) {
	if err := p.expect(tokHash); err != nil {
		return 0, err
	}
	if err := p.expect(tokBang); err != nil {
		return 0, err
	}
	if err := p.expect(tokLBracket); err != nil {
		return 0, err
	}
	if p.tok.kind != tokIdent || p.tok.str != "enable" {
		return 0, p.unexpected("enable")
	}
	if err := p.next(); err != nil {
		return 0, err
	}
	if err := p.expect(tokLParen); err != nil {
		return 0, err
	}
	var exts Extensions
	for p.tok.kind != tokRParen {
		if p.tok.kind != tokIdent {
			return 0, p.unexpected("extension name")
		}
		ext, ok := extensionByName(p.tok.str)
		if !ok {
			return 0, p.errorf("unknown extension %q", p.tok.str)
		}
		exts |= ext
		if err := p.next(); err != nil {
			return 0, err
		}
		if p.tok.kind != tokComma {
			break
		}
		if err := p.next(); err != nil {
			return 0, err
		}
	}
	if err := p.expect(tokRParen); err != nil {
		return 0, err
	}
	if err := p.expect(tokRBracket); err != nil {
		return 0, err
	}
	return exts, nil
}

func (p *parser) parseValue() (Value, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxDepth {
		return nil, p.errorf("exceeded maximum nesting depth of %d", maxDepth)
	}

	t := p.tok
	switch t.kind {
	case tokLParen:
		return p.parseParens("")
	case tokLBracket:
		return p.parseList()
	case tokLBrace:
		return p.parseMap()
	case tokInt:
		v, err := parseIntToken(t)
		if err != nil {
			return nil, p.errorf("%s", err.Error())
		}
		return v, p.next()
	case tokFloat:
		v, err := parseFloatToken(t)
		if err != nil {
			return nil, p.errorf("%s", err.Error())
		}
		return v, p.next()
	case tokString:
		return String(t.str), p.next()
	case tokByteString:
		return Bytes(t.str), p.next()
	case tokChar:
		return Char(t.r), p.next()
	case tokByte:
		return NewInt(int64(t.r)), p.next()
	case tokIdent:
		return p.parseIdentValue()
	}
	return nil, p.unexpected("value")
}

func (p *parser) parseIdentValue() (Value, error) {
	t := p.tok
	if !t.rawID {
		switch t.str {
		case "true":
			return Bool(true), p.next()
		case "false":
			return Bool(false), p.next()
		case "inf":
			return Float(math.Inf(1)), p.next()
		case "NaN":
			return Float(math.NaN()), p.next()
		case "None":
			return None, p.next()
		case "Some":
			if err := p.next(); err != nil {
				return nil, err
			}
			if err := p.expect(tokLParen); err != nil {
				return nil, err
			}
			v, err := p.parseValue()
			if err != nil {
				return nil, err
			}
			if p.tok.kind == tokComma {
				if err := p.next(); err != nil {
					return nil, err
				}
			}
			if err := p.expect(tokRParen); err != nil {
				return nil, err
			}
			return Some(v), nil
		}
	}
	if err := p.next(); err != nil {
		return nil, err
	}
	if p.tok.kind == tokLParen {
		return p.parseParens(t.str)
	}
	return Ident(t.str), nil
}

// parseParens parses (...) which is a unit, tuple or struct depending on the
// contents. The current token is the opening parenthesis.
func (p *parser) parseParens(name string) (Value, error) {
	if err := p.next(); err != nil {
		return nil, err
	}
	if p.tok.kind == tokRParen {
		if err := p.next(); err != nil {
			return nil, err
		}
		if name == "" {
			return Unit{}, nil
		}
		return Tuple{Name: name}, nil
	}
	isStruct := false
	if p.tok.kind == tokIdent {
		nt, err := p.peek()
		if err != nil {
			return nil, err
		}
		isStruct = nt.kind == tokColon
	}
	if isStruct {
		s := Struct{Name: name}
		for p.tok.kind != tokRParen {
			if p.tok.kind != tokIdent {
				return nil, p.unexpected("field name")
			}
			fname := p.tok.str
			if err := p.next(); err != nil {
				return nil, err
			}
			if err := p.expect(tokColon); err != nil {
				return nil, err
			}
			v, err := p.parseValue()
			if err != nil {
				return nil, err
			}
			s.Fields = append(s.Fields, Field{Name: fname, Value: v})
			if p.tok.kind != tokComma {
				break
			}
			if err := p.next(); err != nil {
				return nil, err
			}
		}
		return s, p.expect(tokRParen)
	}
	tup := Tuple{Name: name}
	for p.tok.kind != tokRParen {
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		tup.Values = append(tup.Values, v)
		if p.tok.kind != tokComma {
			break
		}
		if err := p.next(); err != nil {
			return nil, err
		}
	}
	return tup, p.expect(tokRParen)
}

func (p *parser) parseList() (Value, error) {
	if err := p.next(); err != nil {
		return nil, err
	}
	list := List{}
	for p.tok.kind != tokRBracket {
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		list = append(list, v)
		if p.tok.kind != tokComma {
			break
		}
		if err := p.next(); err != nil {
			return nil, err
		}
	}
	return list, p.expect(tokRBracket)
}

func (p *parser) parseMap() (Value, error) {
	if err := p.next(); err != nil {
		return nil, err
	}
	m := Map{}
	for p.tok.kind != tokRBrace {
		k, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		if err := p.expect(tokColon); err != nil {
			return nil, err
		}
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		m = append(m, MapEntry{Key: k, Value: v})
		if p.tok.kind != tokComma {
			break
		}
		if err := p.next(); err != nil {
			return nil, err
		}
	}
	return m, p.expect(tokRBrace)
}

type intRange struct {
	min, max *big.Int
}

func bitRange(bits uint, signed bool) intRange {
	if signed {
		m := new(big.Int).Lsh(big.NewInt(1), bits-1)
		return intRange{min: new(big.Int).Neg(m), max: m.Sub(m, big.NewInt(1))}
	}
	m := new(big.Int).Lsh(big.NewInt(1), bits)
	return intRange{min: new(big.Int), max: m.Sub(m, big.NewInt(1))}
}

var suffixRanges = map[string]intRange{
	"i8": bitRange(8, true), "i16": bitRange(16, true), "i32": bitRange(32, true),
	"i64": bitRange(64, true), "i128": bitRange(128, true), "isize": bitRange(64, true),
	"u8": bitRange(8, false), "u16": bitRange(16, false), "u32": bitRange(32, false),
	"u64": bitRange(64, false), "u128": bitRange(128, false), "usize": bitRange(64, false),
}

type literalError string

func (e literalError) Error() string { return string(e) }

func parseIntToken(t token) (Value, error) {
	s := t.str
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	base := 10
	if len(s) > 2 && s[0] == '0' {
		switch s[1] {
		case 'x':
			base = 16
		case 'o':
			base = 8
		case 'b':
			base = 2
		}
		if base != 10 {
			s = s[2:]
		}
	}
	n, ok := new(big.Int).SetString(s, base)
	if !ok {
		return nil, literalError("invalid integer " + strconv.Quote(t.raw))
	}
	if neg {
		n.Neg(n)
	}
	if t.suffix != "" {
		r := suffixRanges[t.suffix]
		if n.Cmp(r.min) < 0 || n.Cmp(r.max) > 0 {
			return nil, literalError("integer " + t.raw + " out of range for " + t.suffix)
		}
	}
	return Int{n: n}, nil
}

func parseFloatToken(t token) (Value, error) {
	bits := 64
	if t.suffix == "f32" {
		bits = 32
	}
	if strings.TrimLeft(t.str, "+-") == "NaN" {
		return Float(math.NaN()), nil
	}
	f, err := strconv.ParseFloat(t.str, bits)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return Float(f), nil
		}
		return nil, literalError("invalid float " + strconv.Quote(t.raw))
	}
	return Float(f), nil
}
