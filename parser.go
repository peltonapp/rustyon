// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"errors"
	"math"

	"github.com/peltonapp/rustyon-go/internal/syntax"
)

// maxDepth limits nesting to protect against stack exhaustion on hostile
// input.
const maxDepth = syntax.MaxDepth

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

// Names returns the RON names of the enabled extensions.
func (e Extensions) Names() []string {
	var names []string
	for i, name := range syntax.ExtensionNames {
		if e&(1<<i) != 0 {
			names = append(names, name)
		}
	}
	return names
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
	if p.tok.Kind != syntax.EOF {
		return nil, p.unexpected("end of input")
	}
	return doc, nil
}

// Valid reports whether data is a syntactically valid RON document.
func Valid(data []byte) bool {
	_, err := ParseDocument(data)
	return err == nil
}

// syntaxError converts errors from the syntax package into *SyntaxError.
func syntaxError(err error) error {
	var se *syntax.Error
	if errors.As(err, &se) {
		return &SyntaxError{Msg: se.Msg, Pos: Position(se.Pos)}
	}
	return err
}

type parser struct {
	lex      *syntax.Lexer
	tok      syntax.Token // current token, never a comment
	ahead    syntax.Token // one token of lookahead, never a comment
	hasAhead bool
	depth    int
}

func newParser(data []byte) (*parser, error) {
	if err := syntax.CheckUTF8(data); err != nil {
		return nil, syntaxError(err)
	}
	p := &parser{lex: syntax.NewLexer(data)}
	if err := p.next(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *parser) lexNonComment() (syntax.Token, error) {
	for {
		t, err := p.lex.Next()
		if err != nil {
			return t, syntaxError(err)
		}
		if !t.Kind.IsComment() {
			return t, nil
		}
	}
}

func (p *parser) next() error {
	if p.hasAhead {
		p.tok = p.ahead
		p.hasAhead = false
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
func (p *parser) peek() (syntax.Token, error) {
	if !p.hasAhead {
		t, err := p.lexNonComment()
		if err != nil {
			return t, err
		}
		p.ahead, p.hasAhead = t, true
	}
	return p.ahead, nil
}

func (p *parser) errorf(format string, args ...any) error {
	return syntaxError(syntax.Errorf(p.lex.Resolve(p.tok.Pos), format, args...))
}

func (p *parser) unexpected(want string) error {
	return p.errorf("expected %s, found %s", want, p.tok.Describe())
}

func (p *parser) expect(k syntax.Kind) error {
	if p.tok.Kind != k {
		return p.unexpected(k.String())
	}
	return p.next()
}

// expectClose consumes the closing delimiter k after a collection element.
func (p *parser) expectClose(k syntax.Kind) error {
	if p.tok.Kind != k {
		return p.unexpected("',' or " + k.String())
	}
	return p.next()
}

func (p *parser) parseDocument() (*Document, error) {
	doc := &Document{}
	for p.tok.Kind == syntax.Hash {
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
	for _, k := range []syntax.Kind{syntax.Hash, syntax.Bang, syntax.LBracket} {
		if err := p.expect(k); err != nil {
			return 0, err
		}
	}
	if p.tok.Kind != syntax.Ident || p.tok.Str != "enable" {
		return 0, p.unexpected("enable")
	}
	if err := p.next(); err != nil {
		return 0, err
	}
	if err := p.expect(syntax.LParen); err != nil {
		return 0, err
	}
	var exts Extensions
	for p.tok.Kind != syntax.RParen {
		if p.tok.Kind != syntax.Ident {
			return 0, p.unexpected("extension name")
		}
		bit, ok := syntax.ExtensionBit(p.tok.Str)
		if !ok {
			return 0, p.errorf("unknown extension %q", p.tok.Str)
		}
		exts |= 1 << bit
		if err := p.next(); err != nil {
			return 0, err
		}
		if p.tok.Kind != syntax.Comma {
			break
		}
		if err := p.next(); err != nil {
			return 0, err
		}
	}
	if err := p.expect(syntax.RParen); err != nil {
		return 0, err
	}
	if err := p.expect(syntax.RBracket); err != nil {
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
	switch t.Kind {
	case syntax.LParen:
		return p.parseParens("")
	case syntax.LBracket:
		return p.parseList()
	case syntax.LBrace:
		return p.parseMap()
	case syntax.Int:
		small, b, err := syntax.ParseInt(t)
		if err != nil {
			return nil, p.errorf("%s", err.Error())
		}
		return Int{small: small, b: b}, p.next()
	case syntax.Float:
		f, err := syntax.ParseFloat(t)
		if err != nil {
			return nil, p.errorf("%s", err.Error())
		}
		return Float(f), p.next()
	case syntax.String:
		return String(t.Str), p.next()
	case syntax.ByteString:
		return Bytes(t.Str), p.next()
	case syntax.Char:
		return Char(t.Rune), p.next()
	case syntax.Byte:
		return NewInt(int64(t.Rune)), p.next()
	case syntax.Ident:
		return p.parseIdentValue()
	}
	return nil, p.unexpected("value")
}

func (p *parser) parseIdentValue() (Value, error) {
	t := p.tok
	if !t.RawIdent {
		switch t.Str {
		case "true":
			return Bool(true), p.next()
		case "false":
			return Bool(false), p.next()
		case "inf", "inff32", "inff64":
			return Float(math.Inf(1)), p.next()
		case "NaN", "NaNf32", "NaNf64":
			return Float(math.NaN()), p.next()
		case "None":
			return None, p.next()
		case "Some":
			if err := p.next(); err != nil {
				return nil, err
			}
			if err := p.expect(syntax.LParen); err != nil {
				return nil, err
			}
			v, err := p.parseValue()
			if err != nil {
				return nil, err
			}
			if p.tok.Kind == syntax.Comma {
				if err := p.next(); err != nil {
					return nil, err
				}
			}
			if err := p.expect(syntax.RParen); err != nil {
				return nil, err
			}
			return Some(v), nil
		}
	}
	if err := p.next(); err != nil {
		return nil, err
	}
	if p.tok.Kind == syntax.LParen {
		return p.parseParens(t.Str)
	}
	return Ident(t.Str), nil
}

// parseParens parses (...) which is a unit, tuple or struct depending on the
// contents. The current token is the opening parenthesis.
func (p *parser) parseParens(name string) (Value, error) {
	if err := p.next(); err != nil {
		return nil, err
	}
	if p.tok.Kind == syntax.RParen {
		if err := p.next(); err != nil {
			return nil, err
		}
		if name == "" {
			return Unit{}, nil
		}
		return Tuple{Name: name}, nil
	}
	isStruct := false
	if p.tok.Kind == syntax.Ident {
		nt, err := p.peek()
		if err != nil {
			return nil, err
		}
		isStruct = nt.Kind == syntax.Colon
	}
	if isStruct {
		s := Struct{Name: name}
		for p.tok.Kind != syntax.RParen {
			if p.tok.Kind != syntax.Ident {
				return nil, p.unexpected("field name")
			}
			fname := p.tok.Str
			if err := p.next(); err != nil {
				return nil, err
			}
			if err := p.expect(syntax.Colon); err != nil {
				return nil, err
			}
			v, err := p.parseValue()
			if err != nil {
				return nil, err
			}
			s.Fields = append(s.Fields, Field{Name: fname, Value: v})
			if p.tok.Kind != syntax.Comma {
				break
			}
			if err := p.next(); err != nil {
				return nil, err
			}
		}
		return s, p.expectClose(syntax.RParen)
	}
	tup := Tuple{Name: name}
	for p.tok.Kind != syntax.RParen {
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		tup.Values = append(tup.Values, v)
		if p.tok.Kind != syntax.Comma {
			break
		}
		if err := p.next(); err != nil {
			return nil, err
		}
	}
	return tup, p.expectClose(syntax.RParen)
}

func (p *parser) parseList() (Value, error) {
	if err := p.next(); err != nil {
		return nil, err
	}
	list := List{}
	for p.tok.Kind != syntax.RBracket {
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		list = append(list, v)
		if p.tok.Kind != syntax.Comma {
			break
		}
		if err := p.next(); err != nil {
			return nil, err
		}
	}
	return list, p.expectClose(syntax.RBracket)
}

func (p *parser) parseMap() (Value, error) {
	if err := p.next(); err != nil {
		return nil, err
	}
	m := Map{}
	for p.tok.Kind != syntax.RBrace {
		k, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		if err := p.expect(syntax.Colon); err != nil {
			return nil, err
		}
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		m = append(m, MapEntry{Key: k, Value: v})
		if p.tok.Kind != syntax.Comma {
			break
		}
		if err := p.next(); err != nil {
			return nil, err
		}
	}
	return m, p.expectClose(syntax.RBrace)
}
