// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"errors"
	"math"
	"math/big"
	"math/bits"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/peltonapp/rustyon-go/internal/syntax"
)

// maxDepth limits nesting while encoding to protect against cycles.
const maxDepth = syntax.MaxDepth

// recursionLimit is the nesting budget of the parser. It mirrors the default
// recursion limit of ron-rs: every list, map, tuple, struct and every element
// inside them costs one unit, and Some( costs one unit.
const recursionLimit = 128

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
//
// The accepted grammar is the one ron-rs accepts when deserializing into its
// dynamic value type, including its quirks such as ranges (1..5) and the
// recursion limit.
func ParseDocument(data []byte) (*Document, error) {
	p, err := newParser(data)
	if err != nil {
		return nil, err
	}
	doc, err := p.parseDocument()
	if err != nil {
		return nil, p.wrap(err)
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

type errKind uint8

const (
	errOther errKind = iota
	errUnderscore
	errInvalidDigit
	errExpectedString
	errExpectedByteString
)

type parseError struct {
	kind errKind
	msg  string
	off  int
}

func (e *parseError) Error() string { return e.msg }

type parser struct {
	src            string
	cur            int
	exts           Extensions
	newtypeVariant bool
	limit          int
}

func newParser(data []byte) (*parser, error) {
	if err := syntax.CheckUTF8(data); err != nil {
		return nil, syntaxError(err)
	}
	p := &parser{src: string(data), limit: recursionLimit}
	// A leading byte order mark is tolerated although ron-rs rejects it.
	if strings.HasPrefix(p.src, "\xEF\xBB\xBF") {
		p.cur = 3
	}
	return p, nil
}

func (p *parser) wrap(err error) error {
	var pe *parseError
	if errors.As(err, &pe) {
		return &SyntaxError{Msg: pe.msg, Pos: Position(syntax.ResolvePosition(p.src, pe.off))}
	}
	return err
}

func (p *parser) fail(kind errKind, msg string) error {
	return &parseError{kind: kind, msg: msg, off: p.cur}
}

func (p *parser) errEOF() error { return p.fail(errOther, "unexpected end of input") }

func (p *parser) rest() string { return p.src[p.cur:] }

func (p *parser) peekRune() rune {
	if p.cur >= len(p.src) {
		return -1
	}
	if c := p.src[p.cur]; c < utf8.RuneSelf {
		return rune(c)
	}
	r, _ := utf8.DecodeRuneInString(p.src[p.cur:])
	return r
}

func (p *parser) runeAt(off int) rune {
	if off >= len(p.src) {
		return -1
	}
	if c := p.src[off]; c < utf8.RuneSelf {
		return rune(c)
	}
	r, _ := utf8.DecodeRuneInString(p.src[off:])
	return r
}

func (p *parser) peekRuneOrEOF() (rune, error) {
	r := p.peekRune()
	if r < 0 {
		return 0, p.errEOF()
	}
	return r, nil
}

func (p *parser) nextChar() (rune, error) {
	if p.cur >= len(p.src) {
		return 0, p.errEOF()
	}
	if c := p.src[p.cur]; c < utf8.RuneSelf {
		p.cur++
		return rune(c), nil
	}
	r, n := utf8.DecodeRuneInString(p.src[p.cur:])
	p.cur += n
	return r, nil
}

func (p *parser) checkChar(c byte) bool { return p.cur < len(p.src) && p.src[p.cur] == c }

func (p *parser) consumeChar(c byte) bool {
	if p.checkChar(c) {
		p.cur++
		return true
	}
	return false
}

func (p *parser) checkStr(s string) bool { return strings.HasPrefix(p.src[p.cur:], s) }

func (p *parser) consumeStr(s string) bool {
	if p.checkStr(s) {
		p.cur += len(s)
		return true
	}
	return false
}

// identOtherChar reports whether the character i bytes ahead continues an
// identifier.
func (p *parser) identOtherChar(i int) bool {
	r := p.runeAt(p.cur + i)
	return r >= 0 && syntax.IsXIDContinue(r)
}

func (p *parser) checkIdent(id string) bool { return p.checkStr(id) && !p.identOtherChar(len(id)) }

func (p *parser) consumeIdent(id string) bool {
	if p.checkIdent(id) {
		p.cur += len(id)
		return true
	}
	return false
}

// whileFrom returns the length in bytes of the prefix of the remaining
// source, starting at from, whose runes satisfy cond.
func (p *parser) whileFrom(from int, cond func(rune) bool) int {
	i := p.cur + from
	for i < len(p.src) {
		c := p.src[i]
		var r rune
		n := 1
		if c < utf8.RuneSelf {
			r = rune(c)
		} else {
			r, n = utf8.DecodeRuneInString(p.src[i:])
		}
		if !cond(r) {
			break
		}
		i += n
	}
	return i - p.cur - from
}

func isWhitespace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f', 0x85, 0x200E, 0x200F, 0x2028, 0x2029:
		return true
	}
	return false
}

func isIntChar(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') || r == '_'
}

func isFloatChar(r rune) bool {
	return (r >= '0' && r <= '9') || r == 'e' || r == 'E' || r == '.' || r == '+' || r == '-' || r == '_'
}

func isIdentFirst(r rune) bool { return r == '_' || syntax.IsXIDStart(r) }

func isIdentRaw(r rune) bool {
	return r == '.' || r == '+' || r == '-' || syntax.IsXIDContinue(r)
}

func (p *parser) skipWS() error {
	for {
		for p.cur < len(p.src) {
			c := p.src[p.cur]
			if c < utf8.RuneSelf {
				if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f' {
					p.cur++
					continue
				}
				break
			}
			r, n := utf8.DecodeRuneInString(p.src[p.cur:])
			if !isWhitespace(r) {
				break
			}
			p.cur += n
		}
		more, err := p.skipComment()
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
	}
}

func (p *parser) skipComment() (bool, error) {
	if !p.consumeChar('/') {
		return false, nil
	}
	c, err := p.nextChar()
	if err != nil {
		return false, err
	}
	switch c {
	case '/':
		if i := strings.IndexByte(p.rest(), '\n'); i >= 0 {
			p.cur += i
		} else {
			p.cur = len(p.src)
		}
		return true, nil
	case '*':
		level := 1
		for level > 0 {
			i := strings.IndexAny(p.rest(), "/*")
			if i < 0 {
				p.cur = len(p.src)
				return false, p.fail(errOther, "unclosed block comment")
			}
			p.cur += i
			switch {
			case p.consumeStr("/*"):
				level++
			case p.consumeStr("*/"):
				level--
			default:
				p.cur++
			}
		}
		return true, nil
	}
	return false, p.fail(errOther, "unexpected character "+strconv.QuoteRune(c))
}

func (p *parser) comma() (bool, error) {
	if err := p.skipWS(); err != nil {
		return false, err
	}
	if p.consumeChar(',') {
		return true, p.skipWS()
	}
	return false, nil
}

// consumeAll consumes each string in turn, skipping whitespace after every
// one that is present, and reports whether all were present.
func (p *parser) consumeAll(all ...string) (bool, error) {
	ok := true
	for _, s := range all {
		if p.consumeStr(s) {
			if err := p.skipWS(); err != nil {
				return false, err
			}
		} else {
			ok = false
		}
	}
	return ok, nil
}

// ---------------------------------------------------------------------------
// Document and attributes

func (p *parser) parseDocument() (*Document, error) {
	doc := &Document{}
	exts, err := p.parseAttributes()
	if err != nil {
		return nil, err
	}
	doc.Extensions = exts
	v, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	if err := p.skipWS(); err != nil {
		return nil, err
	}
	if p.cur < len(p.src) {
		return nil, p.fail(errOther, "unexpected trailing characters")
	}
	doc.Value = v
	return doc, nil
}

// parseAttributes skips leading whitespace and parses all document
// attributes. The enabled extensions are applied to the parser.
func (p *parser) parseAttributes() (Extensions, error) {
	if err := p.skipWS(); err != nil {
		return 0, err
	}
	var all Extensions
	for {
		ext, ok, err := p.attribute()
		if err != nil {
			return 0, err
		}
		if !ok {
			break
		}
		all |= ext
		p.exts |= ext
		if err := p.skipWS(); err != nil {
			return 0, err
		}
	}
	return all, nil
}

func (p *parser) attribute() (Extensions, bool, error) {
	if !p.checkChar('#') {
		return 0, false, nil
	}
	ok, err := p.consumeAll("#", "!", "[")
	if err != nil {
		return 0, false, err
	}
	if !ok {
		return 0, false, p.fail(errOther, "expected #![")
	}
	if err := p.skipWS(); err != nil {
		return 0, false, err
	}
	switch {
	case p.consumeIdent("enable"):
		if err := p.skipWS(); err != nil {
			return 0, false, err
		}
		if !p.consumeStr("(") {
			return 0, false, p.fail(errOther, "expected '(' after enable")
		}
		if err := p.skipWS(); err != nil {
			return 0, false, err
		}
		exts, err := p.extensionList()
		if err != nil {
			return 0, false, err
		}
		if err := p.skipWS(); err != nil {
			return 0, false, err
		}
		ok, err := p.consumeAll(")", "]")
		if err != nil {
			return 0, false, err
		}
		if !ok {
			return 0, false, p.fail(errOther, "expected ')]' to end the attribute")
		}
		return exts, true, nil
	case p.consumeIdent("type"), p.consumeIdent("schema"):
		if err := p.skipWS(); err != nil {
			return 0, false, err
		}
		if !p.consumeStr("=") {
			return 0, false, p.fail(errOther, "expected '=' in attribute")
		}
		if err := p.skipWS(); err != nil {
			return 0, false, err
		}
		if _, err := p.parseStringLit(); err != nil {
			return 0, false, err
		}
		if err := p.skipWS(); err != nil {
			return 0, false, err
		}
		if !p.consumeStr("]") {
			return 0, false, p.fail(errOther, "expected ']' to end the attribute")
		}
		return 0, true, nil
	}
	return 0, false, p.fail(errOther, "expected attribute enable, type or schema")
}

func (p *parser) extensionList() (Extensions, error) {
	var exts Extensions
	for {
		id, err := p.identifier()
		if err != nil {
			return 0, err
		}
		bit, ok := syntax.ExtensionBit(id)
		if !ok {
			return 0, p.fail(errOther, "unknown extension "+strconv.Quote(id))
		}
		exts |= 1 << bit
		comma, err := p.comma()
		if err != nil {
			return 0, err
		}
		if !comma && p.identOtherChar(0) {
			return 0, p.fail(errOther, "expected ',' between extensions")
		}
		if !comma || !p.identOtherChar(0) {
			break
		}
	}
	return exts, nil
}

// ---------------------------------------------------------------------------
// Identifiers

// skipIdentifier consumes an identifier if there is one. It does not change
// the position otherwise.
func (p *parser) skipIdentifier() (string, bool) {
	if p.cur >= len(p.src) {
		return "", false
	}
	b := p.src[p.cur]
	if b == 'b' || b == 'r' {
		rest := p.rest()
		for _, pre := range stringPrefixes {
			if strings.HasPrefix(rest, pre) {
				return "", false
			}
		}
		if strings.HasPrefix(rest, "r#") {
			l := p.whileFrom(2, isIdentRaw)
			if l > 0 {
				id := rest[2 : 2+l]
				p.cur += 2 + l
				return id, true
			}
			return "", false
		}
	}
	if b < utf8.RuneSelf {
		if !isASCIIIdentStart(b) {
			return "", false
		}
		end := p.xidEnd(p.cur + 1)
		id := p.src[p.cur:end]
		p.cur = end
		return id, true
	}
	if c := p.peekRune(); c >= 0 && isIdentFirst(c) {
		n := utf8.RuneLen(c)
		end := p.xidEnd(p.cur + n)
		id := p.src[p.cur:end]
		p.cur = end
		return id, true
	}
	return "", false
}

// stringPrefixes are the openings of string and byte string literals that
// stop an identifier from starting with b or r.
var stringPrefixes = [...]string{`b"`, `b'`, `br#`, `br"`, `r"`, `r#"`, `r##`}

func isASCIIIdentStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// xidEnd returns the absolute offset where the run of XID_Continue
// characters that starts at the absolute offset i ends.
func (p *parser) xidEnd(i int) int {
	src := p.src
	for i < len(src) {
		c := src[i]
		if c < utf8.RuneSelf {
			if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
				i++
				continue
			}
			return i
		}
		r, n := utf8.DecodeRuneInString(src[i:])
		if !syntax.IsXIDContinue(r) {
			return i
		}
		i += n
	}
	return i
}

// hasRawOrStringPrefix reports whether the input starts with a string
// literal prefix or with r#.
func (p *parser) hasRawOrStringPrefix() bool {
	rest := p.rest()
	for _, pre := range stringPrefixes {
		if strings.HasPrefix(rest, pre) {
			return true
		}
	}
	return strings.HasPrefix(rest, "r#")
}

// identifier parses a strict identifier such as a struct field name.
func (p *parser) identifier() (string, error) {
	// Fast path: a plain ASCII identifier that is neither a string prefix nor
	// followed by a character that makes it a raw-only identifier.
	if p.cur < len(p.src) {
		if b := p.src[p.cur]; isASCIIIdentStart(b) && ((b != 'b' && b != 'r') || !p.hasRawOrStringPrefix()) {
			end := p.xidEnd(p.cur + 1)
			if end >= len(p.src) || (p.src[end] != '.' && p.src[end] != '+' && p.src[end] != '-') {
				id := p.src[p.cur:end]
				p.cur = end
				return id, nil
			}
		}
	}
	first, err := p.peekRuneOrEOF()
	if err != nil {
		return "", err
	}
	if !isIdentFirst(first) {
		if isIdentRaw(first) {
			return "", p.fail(errOther, "invalid identifier, use a raw identifier r#...")
		}
		return "", p.fail(errOther, "expected identifier")
	}
	for _, pre := range []string{`b"`, `b'`, `br#`, `br"`, `r"`, `r#"`, `r##`} {
		if p.checkStr(pre) {
			return "", p.fail(errOther, "expected identifier")
		}
	}
	var length int
	switch {
	case p.checkStr("r#"):
		backup := p.cur
		p.cur += 2
		if r := p.peekRune(); r < 0 || !isIdentRaw(r) {
			p.cur = backup
			return "", p.fail(errOther, "expected identifier")
		}
		length = p.whileFrom(0, isIdentRaw)
	case first == 'r':
		std := p.whileFrom(0, syntax.IsXIDContinue)
		raw := p.whileFrom(0, isIdentRaw)
		if raw > std {
			return "", p.fail(errOther, "invalid identifier, use a raw identifier r#...")
		}
		length = std
	default:
		n := utf8.RuneLen(first)
		std := n + p.whileFrom(n, syntax.IsXIDContinue)
		raw := p.whileFrom(0, isIdentRaw)
		if raw > std {
			return "", p.fail(errOther, "invalid identifier, use a raw identifier r#...")
		}
		length = std
	}
	id := p.src[p.cur : p.cur+length]
	p.cur += length
	return id, nil
}

// ---------------------------------------------------------------------------
// Numbers

type u128 struct{ hi, lo uint64 }

func (a u128) mul(m uint64) (u128, bool) {
	h1, lo := bits.Mul64(a.lo, m)
	h2, hl := bits.Mul64(a.hi, m)
	hi, c := bits.Add64(h1, hl, 0)
	return u128{hi, lo}, h2 != 0 || c != 0
}

func (a u128) add(d uint64) (u128, bool) {
	lo, c := bits.Add64(a.lo, d, 0)
	hi, c2 := bits.Add64(a.hi, 0, c)
	return u128{hi, lo}, c2 != 0
}

func (a u128) gt(b u128) bool { return a.hi > b.hi || (a.hi == b.hi && a.lo > b.lo) }

// intSpec holds the largest magnitude a type can hold for each sign.
type intSpec struct{ posMax, negMax u128 }

func specBits(n uint, signed bool) intSpec {
	var one u128
	if n < 64 {
		one = u128{0, 1 << n}
	} else if n == 64 {
		one = u128{1, 0}
	} else {
		one = u128{1 << (n - 64), 0}
	}
	dec := func(v u128) u128 {
		if v.lo == 0 {
			return u128{v.hi - 1, math.MaxUint64}
		}
		return u128{v.hi, v.lo - 1}
	}
	if !signed {
		if n == 128 {
			return intSpec{posMax: u128{math.MaxUint64, math.MaxUint64}}
		}
		return intSpec{posMax: dec(one)}
	}
	half := u128{one.hi >> 1, one.lo >> 1}
	if one.hi&1 == 1 {
		half.lo |= 1 << 63
	}
	if n == 128 {
		half = u128{1 << 63, 0}
	}
	return intSpec{posMax: dec(half), negMax: half}
}

var intSpecs = map[string]intSpec{
	"i8": specBits(8, true), "i16": specBits(16, true), "i32": specBits(32, true), "i64": specBits(64, true),
	"u8": specBits(8, false), "u16": specBits(16, false), "u32": specBits(32, false), "u64": specBits(64, false),
	"i128": specBits(128, true), "u128": specBits(128, false),
}

var suffixOrder = []string{"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "i128", "u128"}

type number struct {
	isFloat bool
	f       float64
	neg     bool
	mag     u128
}

func (n number) value() Value {
	if n.isFloat {
		return Float(n.f)
	}
	return intValue(n.neg, n.mag)
}

func intValue(neg bool, m u128) Int {
	if m.hi == 0 {
		if !neg {
			if m.lo <= math.MaxInt64 {
				return Int{small: int64(m.lo)}
			}
			return Int{b: new(big.Int).SetUint64(m.lo)}
		}
		if m.lo <= 1<<63 {
			return Int{small: -int64(m.lo)}
		}
		b := new(big.Int).SetUint64(m.lo)
		return Int{b: b.Neg(b)}
	}
	b := new(big.Int).SetUint64(m.hi)
	b.Lsh(b, 64)
	b.Or(b, new(big.Int).SetUint64(m.lo))
	if neg {
		b.Neg(b)
	}
	return Int{b: b}
}

func hexVal(r rune) (int, bool) {
	switch {
	case r >= '0' && r <= '9':
		return int(r - '0'), true
	case r >= 'a' && r <= 'f':
		return int(r-'a') + 10, true
	case r >= 'A' && r <= 'F':
		return int(r-'A') + 10, true
	}
	return 0, false
}

func (p *parser) parseIntegerDigits(s string, base int, neg bool, spec intSpec) (u128, error) {
	bound := spec.posMax
	if neg {
		bound = spec.negMax
	}
	var acc u128
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' {
			continue
		}
		var ovf bool
		acc, ovf = acc.mul(uint64(base))
		if ovf || acc.gt(bound) {
			p.cur += len(s)
			return acc, p.fail(errOther, "integer out of bounds")
		}
		d, _ := hexVal(rune(c))
		if d >= base {
			p.cur += i
			return acc, p.fail(errInvalidDigit, "invalid digit "+strconv.QuoteRune(rune(c))+" for base "+strconv.Itoa(base))
		}
		acc, ovf = acc.add(uint64(d))
		if ovf || acc.gt(bound) {
			p.cur += len(s)
			return acc, p.fail(errOther, "integer out of bounds")
		}
	}
	p.cur += len(s)
	return acc, nil
}

func (p *parser) parseInteger(neg bool, base int, spec intSpec) (number, error) {
	n := p.whileFrom(0, isIntChar)
	if n == 0 {
		return number{}, p.fail(errOther, "expected integer")
	}
	if p.checkChar('_') {
		return number{}, p.fail(errUnderscore, "unexpected leading underscore in a number")
	}
	m, err := p.parseIntegerDigits(p.src[p.cur:p.cur+n], base, neg, spec)
	if err != nil {
		return number{}, err
	}
	return number{neg: neg, mag: m}, nil
}

func (p *parser) integer() (number, error) {
	c, err := p.peekRuneOrEOF()
	if err != nil {
		return number{}, err
	}
	neg := false
	switch {
	case c == '+':
		p.cur++
	case c == '-':
		p.cur++
		neg = true
	case c == 'b' && p.consumeStr("b'"):
		return p.byteLiteral()
	}
	base := 10
	switch {
	case p.consumeStr("0b"):
		base = 2
	case p.consumeStr("0o"):
		base = 8
	case p.consumeStr("0x"):
		base = 16
	}
	n := p.whileFrom(0, isIntChar)
	if r := p.runeAt(p.cur + n); r == 'i' || r == 'u' {
		intCursor := p.cur
		p.cur += n
		for _, suf := range suffixOrder {
			if !p.consumeIdent(suf) {
				continue
			}
			p.cur = intCursor
			res, err := p.parseInteger(neg, base, intSpecs[suf])
			var pe *parseError
			if !errors.As(err, &pe) || (pe.kind != errUnderscore && pe.kind != errInvalidDigit) {
				p.skipIdentifier()
			}
			return res, err
		}
		p.cur = intCursor
	}
	if neg {
		return p.parseInteger(true, base, intSpecs["i128"])
	}
	return p.parseInteger(false, base, intSpecs["u128"])
}

// byteLiteral parses the rest of b'x' after the opening b'.
func (p *parser) byteLiteral() (number, error) {
	c, err := p.nextChar()
	if err != nil {
		return number{}, err
	}
	var b byte
	switch {
	case c == '\\':
		e, err := p.parseEscape(true, true)
		if err != nil {
			return number{}, err
		}
		if !e.ascii {
			return number{}, p.fail(errOther, "unexpected Unicode escape in byte literal")
		}
		b = e.b
	case c < utf8.RuneSelf:
		b = byte(c)
	default:
		return number{}, p.fail(errOther, "expected byte literal")
	}
	if !p.consumeChar('\'') {
		return number{}, p.fail(errOther, "expected byte literal")
	}
	return number{mag: u128{0, uint64(b)}}, nil
}

func isNumberStart(p *parser, c rune) bool {
	return (c >= '0' && c <= '9') || c == '+' || c == '-' || c == '.' || (c == 'b' && p.checkStr("b'"))
}

func (p *parser) checkAnyNumberStart() bool {
	c := p.peekRune()
	if c < 0 {
		return false
	}
	if isNumberStart(p, c) {
		return true
	}
	for _, id := range []string{"inf", "inff32", "inff64", "NaN", "NaNf32", "NaNf64"} {
		if p.checkIdent(id) {
			return true
		}
	}
	return false
}

func (p *parser) nextBytesIsFloat() bool {
	c := p.peekRune()
	if c < 0 {
		return false
	}
	skip := 0
	if c == '+' || c == '-' {
		skip = 1
	}
	raw := p.whileFrom(skip, isFloatChar)
	run := p.src[p.cur+skip : p.cur+skip+raw]
	valid := raw
	if i := strings.Index(run, ".."); i >= 0 && i < raw {
		valid = i
	}
	return valid > p.whileFrom(skip, isIntChar)
}

// fastNumber recognizes the overwhelmingly common plain decimal integers
// and simple floats (digits with an optional fraction) that are followed by a
// delimiter. It reports false for everything else, leaving the position
// unchanged, so that the general path handles all other spellings and
// reports all errors.
func (p *parser) fastNumber() (number, bool) {
	src := p.src
	i := p.cur
	neg := false
	if i < len(src) && (src[i] == '-' || src[i] == '+') {
		neg = src[i] == '-'
		i++
	}
	d0 := i
	var v uint64
	for i < len(src) && src[i] >= '0' && src[i] <= '9' {
		v = v*10 + uint64(src[i]-'0')
		i++
		if i-d0 > 18 {
			return number{}, false
		}
	}
	if i == d0 {
		return number{}, false
	}
	if i < len(src) && src[i] == '.' {
		j := i + 1
		for j < len(src) && src[j] >= '0' && src[j] <= '9' {
			j++
		}
		if j == i+1 || !isNumberDelimiter(src, j) {
			return number{}, false
		}
		f, err := strconv.ParseFloat(src[p.cur:j], 64)
		if err != nil {
			return number{}, false
		}
		p.cur = j
		return number{isFloat: true, f: f}, true
	}
	if !isNumberDelimiter(src, i) {
		return number{}, false
	}
	p.cur = i
	return number{neg: neg, mag: u128{0, v}}, true
}

func isNumberDelimiter(src string, i int) bool {
	if i >= len(src) {
		return true
	}
	switch src[i] {
	case ',', ')', ']', '}', ':', ' ', '\n', '\t', '\r':
		return true
	}
	return false
}

func (p *parser) anyNumber() (number, error) {
	if p.cur < len(p.src) {
		switch p.src[p.cur] {
		case 'i', 'N':
			switch {
			case p.consumeIdent("inf"), p.consumeIdent("inff32"), p.consumeIdent("inff64"):
				return number{isFloat: true, f: math.Inf(1)}, nil
			case p.consumeIdent("NaN"), p.consumeIdent("NaNf32"), p.consumeIdent("NaNf64"):
				return number{isFloat: true, f: math.NaN()}, nil
			}
		default:
			if n, ok := p.fastNumber(); ok {
				return n, nil
			}
		}
	}
	if p.nextBytesIsFloat() {
		return p.float()
	}
	backup := p.cur
	n, ierr := p.integer()
	if ierr == nil {
		return n, nil
	}
	integerCursor := p.cur
	p.cur = backup
	f, ferr := p.float()
	if ferr == nil && p.cur >= integerCursor {
		return f, nil
	}
	p.cur = integerCursor
	return number{}, ierr
}

func parseFloatStr(s string, bitSize int) (float64, bool) {
	f, err := strconv.ParseFloat(s, bitSize)
	if err != nil {
		var ne *strconv.NumError
		if errors.As(err, &ne) && errors.Is(ne.Err, strconv.ErrRange) {
			return f, true
		}
		return 0, false
	}
	return f, true
}

// floatLiteral parses inf and NaN with an optional sign and f32 or f64 suffix.
func (p *parser) floatLiteral() (number, bool) {
	lits := [...]struct {
		s string
		v float64
	}{
		{"inf", math.Inf(1)}, {"+inf", math.Inf(1)}, {"-inf", math.Inf(-1)},
		{"NaN", math.NaN()}, {"+NaN", math.NaN()}, {"-NaN", math.NaN()},
	}
	for _, l := range lits {
		if p.consumeIdent(l.s) {
			return number{isFloat: true, f: l.v}, true
		}
		if strings.HasPrefix(p.rest(), l.s) {
			after := p.rest()[len(l.s):]
			for _, suf := range []string{"f32", "f64"} {
				if post, ok := strings.CutPrefix(after, suf); ok {
					r, _ := utf8.DecodeRuneInString(post)
					if post == "" || !syntax.IsXIDContinue(r) {
						p.cur += len(l.s) + len(suf)
						return number{isFloat: true, f: l.v}, true
					}
				}
			}
		}
	}
	return number{}, false
}

func (p *parser) float() (number, error) {
	if p.cur < len(p.src) {
		c := p.src[p.cur]
		if c == '+' || c == '-' {
			c = 0
			if p.cur+1 < len(p.src) {
				c = p.src[p.cur+1]
			}
		}
		if c == 'i' || c == 'N' {
			if n, ok := p.floatLiteral(); ok {
				return n, nil
			}
		}
	}
	raw := p.whileFrom(0, isFloatChar)
	src := p.rest()[:raw]
	numBytes := raw
	if i := strings.Index(src, ".."); i >= 0 {
		numBytes = i
	}
	if numBytes == 0 {
		return number{}, p.fail(errOther, "expected float")
	}
	if p.checkChar('_') {
		return number{}, p.fail(errUnderscore, "unexpected leading underscore in a number")
	}
	f := src[:numBytes]
	if strings.IndexByte(f, '_') >= 0 {
		var sb strings.Builder
		allowUnderscore := false
		for i := 0; i < numBytes; i++ {
			c := src[i]
			switch {
			case c == '_' && allowUnderscore:
				continue
			case c == '_':
				p.cur += i
				return number{}, p.fail(errOther, "unexpected underscore in float")
			case (c >= '0' && c <= '9') || c == 'e' || c == 'E':
				allowUnderscore = true
			case c == '.':
				allowUnderscore = false
			}
			sb.WriteByte(c)
		}
		f = sb.String()
	}
	if p.runeAt(p.cur+numBytes) == 'f' {
		backup := p.cur
		p.cur += numBytes
		for _, suf := range []struct {
			s    string
			bits int
		}{{"f32", 32}, {"f64", 64}} {
			if !p.consumeIdent(suf.s) {
				continue
			}
			v, ok := parseFloatStr(f, suf.bits)
			if !ok {
				p.cur = backup
				return number{}, p.fail(errOther, "expected float")
			}
			return number{isFloat: true, f: v}, nil
		}
		p.cur = backup
	}
	v, ok := parseFloatStr(f, 64)
	if !ok {
		return number{}, p.fail(errOther, "expected float")
	}
	p.cur += numBytes
	return number{isFloat: true, f: v}, nil
}

// ---------------------------------------------------------------------------
// Strings, chars and escapes

type escape struct {
	ascii bool
	b     byte
	r     rune
}

func (p *parser) decodeASCIIEscape() (byte, error) {
	var n byte
	for range 2 {
		c, err := p.nextChar()
		if err != nil {
			return 0, err
		}
		d, ok := hexVal(c)
		if !ok {
			return 0, p.fail(errOther, "non-hex digit found in escape")
		}
		n = n<<4 | byte(d)
	}
	return n, nil
}

func (p *parser) parseEscape(binary, isChar bool) (escape, error) {
	c, err := p.nextChar()
	if err != nil {
		return escape{}, err
	}
	switch c {
	case '\'', '"', '\\':
		return escape{ascii: true, b: byte(c)}, nil
	case 'n':
		return escape{ascii: true, b: '\n'}, nil
	case 'r':
		return escape{ascii: true, b: '\r'}, nil
	case 't':
		return escape{ascii: true, b: '\t'}, nil
	case '0':
		return escape{ascii: true, b: 0}, nil
	case 'x':
		b, err := p.decodeASCIIEscape()
		if err != nil {
			return escape{}, err
		}
		if binary {
			return escape{ascii: true, b: b}, nil
		}
		if b < 0x80 {
			return escape{r: rune(b)}, nil
		}
		if isChar {
			return escape{}, p.fail(errOther, "not a valid byte-escaped Unicode character")
		}
		buf := [4]byte{b}
		for i := 1; i < 4; i++ {
			if !p.consumeStr(`\x`) {
				return escape{}, p.fail(errOther, "not a valid byte-escaped Unicode character")
			}
			nb, err := p.decodeASCIIEscape()
			if err != nil {
				return escape{}, err
			}
			buf[i] = nb
			if utf8.Valid(buf[:i+1]) {
				r, _ := utf8.DecodeRune(buf[:i+1])
				return escape{r: r}, nil
			}
		}
		return escape{}, p.fail(errOther, "not a valid byte-escaped Unicode character")
	case 'u':
		if !p.consumeChar('{') {
			return escape{}, p.fail(errOther, "missing { in Unicode escape")
		}
		var v uint32
		digits := 0
		for digits < 6 {
			r, err := p.peekRuneOrEOF()
			if err != nil {
				return escape{}, err
			}
			if r == '}' {
				break
			}
			p.cur += utf8.RuneLen(r)
			digits++
			d, ok := hexVal(r)
			if !ok {
				return escape{}, p.fail(errOther, "non-hex digit found in escape")
			}
			v = v<<4 | uint32(d)
		}
		if digits == 0 {
			return escape{}, p.fail(errOther, "expected 1-6 digits in Unicode escape")
		}
		if !p.consumeChar('}') {
			return escape{}, p.fail(errOther, "no } at the end of Unicode escape")
		}
		if v > utf8.MaxRune || (v >= 0xD800 && v <= 0xDFFF) {
			return escape{}, p.fail(errOther, "not a valid Unicode-escaped character")
		}
		return escape{r: rune(v)}, nil
	}
	return escape{}, p.fail(errOther, "unknown escape character")
}

// consumeStringContinuation consumes a line ending after a backslash and the
// whitespace that follows it.
func (p *parser) consumeStringContinuation() bool {
	switch {
	case p.consumeStr("\r\n"):
	case p.consumeChar('\n'):
	default:
		return false
	}
	for p.cur < len(p.src) {
		c := p.src[p.cur]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		p.cur++
	}
	return true
}

// escapedBytes reads an escaped string body after the opening quote.
func (p *parser) escapedBytes(binary bool) ([]byte, error) {
	if strings.IndexByte(p.rest(), '"') < 0 {
		return nil, p.fail(errOther, "unterminated string")
	}
	var buf []byte
	for {
		rest := p.rest()
		j := strings.IndexAny(rest, "\"\\")
		if j < 0 {
			return nil, p.fail(errOther, "unterminated string")
		}
		buf = append(buf, rest[:j]...)
		p.cur += j
		if p.src[p.cur] == '"' {
			p.cur++
			return buf, nil
		}
		p.cur++
		if p.consumeStringContinuation() {
			continue
		}
		e, err := p.parseEscape(binary, false)
		if err != nil {
			return nil, err
		}
		if e.ascii {
			buf = append(buf, e.b)
		} else {
			buf = utf8.AppendRune(buf, e.r)
		}
	}
}

// rawBytes reads a raw string body after the leading r or br.
func (p *parser) rawBytes() (string, error) {
	hashes := p.whileFrom(0, func(r rune) bool { return r == '#' })
	p.cur += hashes
	if !p.consumeChar('"') {
		return "", p.fail(errExpectedString, "expected '\"' to start raw string")
	}
	ending := "\"" + strings.Repeat("#", hashes)
	i := strings.Index(p.rest(), ending)
	if i < 0 {
		return "", p.fail(errOther, "unterminated raw string")
	}
	s := p.rest()[:i]
	p.cur += i + len(ending)
	return s, nil
}

// parseStringLit parses a string literal. The result may be a substring of
// the source, so callers that keep it beyond the parse must copy it.
func (p *parser) parseStringLit() (string, error) {
	switch {
	case p.consumeChar('"'):
		rest := p.rest()
		j := strings.IndexByte(rest, '"')
		if j < 0 {
			return "", p.fail(errOther, "unterminated string")
		}
		if strings.IndexByte(rest[:j], '\\') < 0 {
			p.cur += j + 1
			return rest[:j], nil
		}
		b, err := p.escapedBytes(false)
		return string(b), err
	case p.consumeChar('r'):
		return p.rawBytes()
	}
	return "", p.fail(errExpectedString, "expected string")
}

func (p *parser) parseByteStringNoBase64() ([]byte, error) {
	switch {
	case p.consumeStr(`b"`):
		return p.escapedBytes(true)
	case p.consumeStr("br"):
		s, err := p.rawBytes()
		if err != nil {
			var pe *parseError
			if errors.As(err, &pe) && pe.kind == errExpectedString {
				return nil, p.fail(errExpectedByteString, "expected byte string")
			}
			return nil, err
		}
		return []byte(s), nil
	}
	return nil, p.fail(errExpectedByteString, "expected byte string")
}

func (p *parser) parseChar() (rune, error) {
	if !p.consumeChar('\'') {
		return 0, p.fail(errOther, "expected char")
	}
	c, err := p.nextChar()
	if err != nil {
		return 0, err
	}
	if c == '\\' {
		e, err := p.parseEscape(false, true)
		if err != nil {
			return 0, err
		}
		if e.ascii {
			c = rune(e.b)
		} else {
			c = e.r
		}
	}
	if !p.consumeChar('\'') {
		return 0, p.fail(errOther, "expected closing ' of char literal")
	}
	return c, nil
}

// ---------------------------------------------------------------------------
// Struct type detection

type structType uint8

const (
	stAnyTuple structType = iota
	stEmptyTuple
	stNewtypeTuple
	stNonNewtypeTuple
	stNamed
	stUnit
)

func (p *parser) checkStructType(noParensMeanUnit, differentiate bool) (structType, error) {
	backup := p.cur
	st, err := p.checkStructTypeInner(noParensMeanUnit, differentiate)
	if err == nil {
		p.cur = backup
	}
	return st, err
}

func (p *parser) checkStructTypeInner(noParensMeanUnit, differentiate bool) (structType, error) {
	if noParensMeanUnit && !p.consumeChar('(') {
		return stUnit, nil
	}
	if err := p.skipWS(); err != nil {
		return 0, err
	}
	if noParensMeanUnit && p.checkChar(')') {
		return stEmptyTuple, nil
	}
	if _, ok := p.skipIdentifier(); ok {
		if err := p.skipWS(); err != nil {
			return 0, err
		}
		switch p.peekRune() {
		case ':':
			return stNamed, nil
		case ',':
			p.cur++
			if err := p.skipWS(); err != nil {
				return 0, err
			}
			if p.checkChar(')') {
				return stNewtypeTuple, nil
			}
			return stNonNewtypeTuple, nil
		case ')':
			return stNewtypeTuple, nil
		}
	}
	if !differentiate {
		return stAnyTuple, nil
	}
	braces := 1
	moreThanOne := false
	for braces > 0 {
		if err := p.skipWS(); err != nil {
			return 0, err
		}
		backup := p.cur
		if _, err := p.parseChar(); err != nil {
			p.cur = backup
		}
		backup = p.cur
		if _, err := p.parseStringLit(); err != nil {
			var pe *parseError
			if errors.As(err, &pe) && pe.kind == errExpectedString {
				p.cur = backup
			} else {
				return 0, err
			}
		}
		backup = p.cur
		if _, err := p.parseByteStringNoBase64(); err != nil {
			var pe *parseError
			if errors.As(err, &pe) && pe.kind == errExpectedByteString {
				p.cur = backup
			} else {
				return 0, err
			}
		}
		if err := p.skipWS(); err != nil {
			return 0, err
		}
		c, err := p.nextChar()
		if err != nil {
			return 0, err
		}
		switch {
		case c == '(' || c == '[' || c == '{':
			braces++
		case c == ')' || c == ']' || c == '}':
			braces--
		case c == ',' && braces == 1:
			if err := p.skipWS(); err != nil {
				return 0, err
			}
			moreThanOne = !p.checkChar(')')
			braces = -1
		}
		if braces < 0 {
			break
		}
	}
	if moreThanOne {
		return stNonNewtypeTuple, nil
	}
	return stNewtypeTuple, nil
}

// ---------------------------------------------------------------------------
// Values

func (p *parser) errRecursion() error {
	return p.fail(errOther, "exceeded the maximum nesting depth")
}

// guarded parses a value inside one unit of the recursion budget.
func (p *parser) guarded() (Value, error) {
	if p.limit == 0 {
		return nil, p.errRecursion()
	}
	p.limit--
	v, err := p.parseValue()
	p.limit++
	return v, err
}

func (p *parser) enter() error {
	if p.limit == 0 {
		return p.errRecursion()
	}
	p.limit--
	return nil
}

func (p *parser) leave() { p.limit++ }

func rangeMap(start, end number) Value {
	return Map{{String("start"), start.value()}, {String("end"), end.value()}}
}

func (p *parser) floatRangeOrValue(start number) (Value, error) {
	if p.consumeStr("..=") {
		end, err := p.anyNumber()
		if err != nil {
			return nil, err
		}
		return rangeMap(start, end), nil
	}
	if p.consumeStr("..") {
		if !p.checkAnyNumberStart() {
			return Map{{String("start"), start.value()}}, nil
		}
		end, err := p.anyNumber()
		if err != nil {
			return nil, err
		}
		return rangeMap(start, end), nil
	}
	return start.value(), nil
}

// hkind is the kind of value found by classify.
type hkind uint8

const (
	hBool hkind = iota
	hNone
	hSome   // "Some" consumed, the payload follows
	hUnit   // ()
	hNum    // an integer or float, in header.num
	hRange  // a range such as 1..5, the position is back at the start
	hIdent  // a bare identifier, in header.s
	hStruct // a struct, positioned at the opening parenthesis
	hTuple  // a tuple, positioned at the opening parenthesis
	hList   // positioned at [
	hMap    // positioned at {
	hBytes  // a byte string, in header.bytes
	hString // a string, in header.s
	hChar   // a char, in header.r
)

// header describes the value at the current position. classify consumes the
// parts of a value that decide what it is and leaves collections open, so
// that the same scanning drives both the tree builder and the streaming
// decoder.
type header struct {
	kind  hkind
	b     bool   // hBool
	old   bool   // hStruct, hTuple: no parentheses, opened by an enclosing Some(
	s     string // hIdent, hString: the text; hStruct, hTuple: the name
	num   number
	r     rune
	bytes []byte
}

// classifyFloat finishes inf and NaN, which can start a range.
func (p *parser) classifyFloat(h *header, start int, f float64) {
	if p.checkStr("..") {
		p.cur = start
		h.kind = hRange
		return
	}
	h.kind = hNum
	h.num = number{isFloat: true, f: f}
}

// classify mirrors deserialize_any of ron-rs. It parses scalars completely
// and stops after the head of collections.
func (p *parser) classify(h *header) error {
	if p.newtypeVariant {
		if p.checkChar(')') {
			p.newtypeVariant = false
			h.kind = hUnit
			return nil
		}
		st, err := p.checkStructType(false, true)
		if err != nil {
			return err
		}
		switch st {
		case stNamed:
			h.kind, h.old, h.s = hStruct, true, ""
			return nil
		case stEmptyTuple, stNonNewtypeTuple:
			h.kind, h.old, h.s = hTuple, true, ""
			return nil
		default:
			p.newtypeVariant = false
		}
	}
	if p.cur >= len(p.src) {
		return p.errEOF()
	}
	start := p.cur
	switch p.src[p.cur] {
	case 't':
		if p.consumeIdent("true") {
			h.kind, h.b = hBool, true
			return nil
		}
	case 'f':
		if p.consumeIdent("false") {
			h.kind, h.b = hBool, false
			return nil
		}
	case 'S':
		if p.checkIdent("Some") {
			p.cur += len("Some")
			h.kind = hSome
			return nil
		}
	case 'N':
		if p.consumeIdent("None") {
			h.kind = hNone
			return nil
		}
		if p.consumeIdent("NaN") || p.consumeIdent("NaNf32") || p.consumeIdent("NaNf64") {
			p.classifyFloat(h, start, math.NaN())
			return nil
		}
	case 'i':
		if p.consumeIdent("inf") || p.consumeIdent("inff32") || p.consumeIdent("inff64") {
			p.classifyFloat(h, start, math.Inf(1))
			return nil
		}
	case '(':
		if p.consumeStr("()") {
			h.kind = hUnit
			return nil
		}
	}
	if name, ok := p.skipIdentifier(); ok {
		if err := p.skipWS(); err != nil {
			return err
		}
		return p.classifyStruct(h, name, true)
	}
	c := rune(p.src[p.cur])
	if c >= utf8.RuneSelf {
		c = p.peekRune()
	}
	switch {
	case c == '(':
		return p.classifyStruct(h, "", false)
	case c == '[':
		h.kind = hList
	case c == '{':
		h.kind = hMap
	case (c >= '0' && c <= '9') || c == '+' || c == '-' || c == '.' || c == 'b':
		if c == 'b' && !p.checkStr("b'") {
			b, err := p.parseByteStringNoBase64()
			h.kind, h.bytes = hBytes, b
			return err
		}
		if p.checkStr("..") {
			h.kind = hRange
			return nil
		}
		n, err := p.anyNumber()
		if err != nil {
			return err
		}
		if p.checkStr("..") {
			p.cur = start
			h.kind = hRange
			return nil
		}
		h.kind, h.num = hNum, n
	case c == '"' || c == 'r':
		s, err := p.parseStringLit()
		h.kind, h.s = hString, s
		return err
	case c == '\'':
		r, err := p.parseChar()
		h.kind, h.r = hChar, r
		return err
	default:
		return p.fail(errOther, "unexpected character "+strconv.QuoteRune(c))
	}
	return nil
}

// classifyStruct decides between a bare identifier, a struct and a tuple
// after an optional name.
func (p *parser) classifyStruct(h *header, name string, hasName bool) error {
	st, err := p.checkStructType(true, false)
	if err != nil {
		return err
	}
	h.s, h.old = name, false
	switch st {
	case stUnit:
		if hasName {
			h.kind = hIdent
		} else {
			h.kind, h.s = hUnit, ""
		}
	case stNamed:
		h.kind = hStruct
	default:
		h.kind = hTuple
	}
	return nil
}

// rangeValue builds the map ron-rs deserializes a range into. The position
// is at the start of the range.
func (p *parser) rangeValue() (Value, error) {
	switch {
	case p.consumeIdent("inf"), p.consumeIdent("inff32"), p.consumeIdent("inff64"):
		return p.floatRangeOrValue(number{isFloat: true, f: math.Inf(1)})
	case p.consumeIdent("NaN"), p.consumeIdent("NaNf32"), p.consumeIdent("NaNf64"):
		return p.floatRangeOrValue(number{isFloat: true, f: math.NaN()})
	}
	if p.consumeStr("..=") {
		end, err := p.anyNumber()
		if err != nil {
			return nil, err
		}
		return Map{{String("end"), end.value()}}, nil
	}
	if p.consumeStr("..") {
		if p.checkAnyNumberStart() {
			end, err := p.anyNumber()
			if err != nil {
				return nil, err
			}
			return Map{{String("end"), end.value()}}, nil
		}
		return Unit{}, nil
	}
	start, err := p.anyNumber()
	if err != nil {
		return nil, err
	}
	return p.floatRangeOrValue(start)
}

// parseValue parses any value into the dynamic tree.
func (p *parser) parseValue() (Value, error) {
	var h header
	if err := p.classify(&h); err != nil {
		return nil, err
	}
	switch h.kind {
	case hBool:
		return Bool(h.b), nil
	case hNone:
		return None, nil
	case hSome:
		return p.parseOption()
	case hUnit:
		return Unit{}, nil
	case hNum:
		return h.num.value(), nil
	case hRange:
		return p.rangeValue()
	case hIdent:
		return Ident(h.s), nil
	case hStruct:
		return p.parseStruct(&h)
	case hTuple:
		return p.parseTuple(&h)
	case hList:
		return p.parseList()
	case hMap:
		return p.parseMap()
	case hBytes:
		return Bytes(h.bytes), nil
	case hString:
		return String(strings.Clone(h.s)), nil
	}
	return Char(h.r), nil
}

// parseOption parses the payload of Some after the Some keyword.
func (p *parser) parseOption() (Value, error) {
	if err := p.skipWS(); err != nil {
		return nil, err
	}
	if p.consumeChar('(') {
		if err := p.skipWS(); err != nil {
			return nil, err
		}
		p.newtypeVariant = p.exts&UnwrapVariantNewtypes != 0
		v, err := p.guarded()
		if err != nil {
			return nil, err
		}
		p.newtypeVariant = false
		if _, err := p.comma(); err != nil {
			return nil, err
		}
		if !p.consumeChar(')') {
			return nil, p.fail(errOther, "expected ')' to end Some")
		}
		return Some(v), nil
	}
	if p.exts&ImplicitSome != 0 {
		v, err := p.guarded()
		if err != nil {
			return nil, err
		}
		return Some(v), nil
	}
	return nil, p.fail(errOther, "expected '(' after Some")
}

// hasElement reports whether another element follows in a comma separated
// collection that ends with the byte term.
func (p *parser) hasElement(hadComma bool, term byte) (bool, error) {
	if err := p.skipWS(); err != nil {
		return false, err
	}
	atEnd := p.checkChar(term)
	if hadComma {
		return !atEnd, nil
	}
	if atEnd {
		return false, nil
	}
	return false, p.fail(errOther, "expected ',' or '"+string(term)+"'")
}

func (p *parser) parseStruct(h *header) (Value, error) {
	if !h.old && !p.consumeChar('(') {
		return nil, p.fail(errOther, "expected struct")
	}
	old := h.old
	p.newtypeVariant = false
	if err := p.enter(); err != nil {
		return nil, err
	}
	s := Struct{Name: h.s}
	hadComma := true
	for {
		ok, err := p.hasElement(hadComma, ')')
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		if p.limit == 0 {
			return nil, p.errRecursion()
		}
		fname, err := p.identifier()
		if err != nil {
			return nil, err
		}
		if err := p.skipWS(); err != nil {
			return nil, err
		}
		if !p.consumeChar(':') {
			return nil, p.fail(errOther, "expected ':' after field name")
		}
		if err := p.skipWS(); err != nil {
			return nil, err
		}
		v, err := p.guarded()
		if err != nil {
			return nil, err
		}
		s.Fields = append(s.Fields, Field{Name: fname, Value: v})
		if hadComma, err = p.comma(); err != nil {
			return nil, err
		}
	}
	p.leave()
	if err := p.skipWS(); err != nil {
		return nil, err
	}
	if old || p.consumeChar(')') {
		return s, nil
	}
	return nil, p.fail(errOther, "expected ')' to end struct")
}

func (p *parser) parseTuple(h *header) (Value, error) {
	if !h.old && !p.consumeChar('(') {
		return nil, p.fail(errOther, "expected tuple")
	}
	old := h.old
	p.newtypeVariant = false
	if err := p.enter(); err != nil {
		return nil, err
	}
	t := Tuple{Name: h.s}
	hadComma := true
	for {
		ok, err := p.hasElement(hadComma, ')')
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		v, err := p.guarded()
		if err != nil {
			return nil, err
		}
		t.Values = append(t.Values, v)
		if hadComma, err = p.comma(); err != nil {
			return nil, err
		}
	}
	p.leave()
	if err := p.skipWS(); err != nil {
		return nil, err
	}
	if old || p.consumeChar(')') {
		return t, nil
	}
	return nil, p.fail(errOther, "expected ')' to end tuple")
}

func (p *parser) parseList() (Value, error) {
	p.newtypeVariant = false
	p.cur++ // '['
	if err := p.enter(); err != nil {
		return nil, err
	}
	list := List{}
	hadComma := true
	for {
		ok, err := p.hasElement(hadComma, ']')
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		v, err := p.guarded()
		if err != nil {
			return nil, err
		}
		list = append(list, v)
		if hadComma, err = p.comma(); err != nil {
			return nil, err
		}
	}
	p.leave()
	if err := p.skipWS(); err != nil {
		return nil, err
	}
	if !p.consumeChar(']') {
		return nil, p.fail(errOther, "expected ']' to end list")
	}
	return list, nil
}

func (p *parser) parseMap() (Value, error) {
	p.newtypeVariant = false
	p.cur++ // '{'
	if err := p.enter(); err != nil {
		return nil, err
	}
	m := Map{}
	hadComma := true
	for {
		ok, err := p.hasElement(hadComma, '}')
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		k, err := p.guarded()
		if err != nil {
			return nil, err
		}
		if err := p.skipWS(); err != nil {
			return nil, err
		}
		if !p.consumeChar(':') {
			return nil, p.fail(errOther, "expected ':' after map key")
		}
		if err := p.skipWS(); err != nil {
			return nil, err
		}
		v, err := p.guarded()
		if err != nil {
			return nil, err
		}
		m = append(m, MapEntry{Key: k, Value: v})
		if hadComma, err = p.comma(); err != nil {
			return nil, err
		}
	}
	p.leave()
	if err := p.skipWS(); err != nil {
		return nil, err
	}
	if !p.consumeChar('}') {
		return nil, p.fail(errOther, "expected '}' to end map")
	}
	return m, nil
}
