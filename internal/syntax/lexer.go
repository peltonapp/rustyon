// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

// Package syntax holds the RON lexer, literal decoding and the comment
// preserving formatter. The public API lives in the root rustyon package.
package syntax

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxDepth limits nesting to protect against stack exhaustion on hostile
// input.
const MaxDepth = 1000

// Kind is the type of a token.
type Kind uint8

// Token kinds.
const (
	EOF Kind = iota
	LParen
	RParen
	LBracket
	RBracket
	LBrace
	RBrace
	Comma
	Colon
	Hash
	Bang
	Ident
	Int
	Float
	String
	ByteString
	Char
	Byte
	LineComment
	BlockComment
)

var kindNames = [...]string{
	EOF:          "end of input",
	LParen:       "'('",
	RParen:       "')'",
	LBracket:     "'['",
	RBracket:     "']'",
	LBrace:       "'{'",
	RBrace:       "'}'",
	Comma:        "','",
	Colon:        "':'",
	Hash:         "'#'",
	Bang:         "'!'",
	Ident:        "identifier",
	Int:          "integer",
	Float:        "float",
	String:       "string",
	ByteString:   "byte string",
	Char:         "char",
	Byte:         "byte literal",
	LineComment:  "comment",
	BlockComment: "comment",
}

func (k Kind) String() string { return kindNames[k] }

// IsComment reports whether the kind is a line or block comment.
func (k Kind) IsComment() bool { return k == LineComment || k == BlockComment }

// Position is a location in the source. Line and Column are 1-based;
// Column counts Unicode code points. Offset is the 0-based byte offset.
type Position struct {
	Offset int
	Line   int
	Column int
}

// Error is a syntax error at a position.
type Error struct {
	Msg string
	Pos Position
}

func (e *Error) Error() string { return fmt.Sprintf("%d:%d: %s", e.Pos.Line, e.Pos.Column, e.Msg) }

// Errorf returns an *Error at p.
func Errorf(p Position, format string, args ...any) *Error {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	return &Error{Msg: msg, Pos: p}
}

// Token is a lexical token.
type Token struct {
	Kind Kind
	Pos  Position
	End  int    // byte offset just past the token
	Raw  string // the token's source text

	// Decoded payload. For identifiers this is the name without any r#
	// prefix, for numbers the literal without underscores and suffix, for
	// strings and byte strings the decoded contents.
	Str      string
	Suffix   string // numeric type suffix such as "u8" or "f32"
	Rune     rune   // value of a char or byte literal
	RawIdent bool   // identifier was written as r#name

	Newlines int // newlines in the whitespace before the token
}

// Describe returns a short description of the token for error messages.
func (t Token) Describe() string {
	switch t.Kind {
	case EOF:
		return "end of input"
	case Ident:
		return "identifier " + t.Raw
	case Int, Float, String, ByteString, Char, Byte:
		raw := t.Raw
		if len(raw) > 32 {
			raw = raw[:29] + "..."
		}
		return t.Kind.String() + " " + raw
	}
	return t.Kind.String()
}

// Lexer splits RON source into tokens.
//
// To keep lexing fast, token positions carry only the byte offset. Line
// and column are computed by [Lexer.Resolve] when an error is reported;
// errors returned by [Lexer.Next] are already resolved.
type Lexer struct {
	src string
	off int
}

// NewLexer returns a lexer for src. A leading UTF-8 byte order mark is
// skipped.
func NewLexer(src []byte) *Lexer {
	l := &Lexer{src: string(src)}
	if strings.HasPrefix(l.src, "\xEF\xBB\xBF") {
		l.off = 3
	}
	return l
}

// Pos returns the current position. Only the offset is set.
func (l *Lexer) Pos() Position { return Position{Offset: l.off} }

// Resolve fills in the line and column of a position from its offset.
func (l *Lexer) Resolve(p Position) Position {
	return ResolvePosition(l.src, p.Offset)
}

// ResolvePosition computes the full position of a byte offset in src.
func ResolvePosition(src string, off int) Position {
	off = min(off, len(src))
	head := src[:off]
	line := 1 + strings.Count(head, "\n")
	lineStart := strings.LastIndexByte(head, '\n') + 1
	if line == 1 && strings.HasPrefix(head, "\xEF\xBB\xBF") {
		lineStart = 3
	}
	col := 1 + utf8.RuneCountInString(head[lineStart:])
	return Position{Offset: off, Line: line, Column: col}
}

// CheckUTF8 returns an error at the first invalid UTF-8 sequence in src.
func CheckUTF8(src []byte) *Error {
	if utf8.Valid(src) {
		return nil
	}
	s := string(src)
	off := 0
	for off < len(s) {
		r, size := utf8.DecodeRuneInString(s[off:])
		if r == utf8.RuneError && size == 1 {
			break
		}
		off += size
	}
	return Errorf(ResolvePosition(s, off), "invalid UTF-8 encoding")
}

// peekAt returns the rune at off+n bytes, or -1 at end of input.
func (l *Lexer) peekAt(n int) rune {
	if l.off+n >= len(l.src) {
		return -1
	}
	c := l.src[l.off+n]
	if c < utf8.RuneSelf {
		return rune(c)
	}
	r, _ := utf8.DecodeRuneInString(l.src[l.off+n:])
	return r
}

func (l *Lexer) peek() rune { return l.peekAt(0) }

// advance consumes one rune and returns it.
func (l *Lexer) advance() rune {
	if l.off >= len(l.src) {
		return -1
	}
	if c := l.src[l.off]; c < utf8.RuneSelf {
		l.off++
		return rune(c)
	}
	r, size := utf8.DecodeRuneInString(l.src[l.off:])
	l.off += size
	return r
}

func isWhitespace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f', 0x85, 0x200E, 0x200F, 0x2028, 0x2029:
		return true
	}
	return false
}

// IsIdentStart reports whether r can start an identifier.
func IsIdentStart(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= utf8.RuneSelf && unicode.IsLetter(r))
}

// IsIdentContinue reports whether r can continue an identifier.
func IsIdentContinue(r rune) bool {
	return IsIdentStart(r) || (r >= '0' && r <= '9') || (r >= utf8.RuneSelf && (unicode.IsDigit(r) || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Mc, r) || unicode.Is(unicode.Pc, r)))
}

// IsRawIdentContinue reports whether r can appear in a raw identifier.
func IsRawIdentContinue(r rune) bool {
	return IsIdentContinue(r) || r == '.' || r == '+' || r == '-'
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

// Next returns the next token, including comments.
func (l *Lexer) Next() (Token, error) {
	newlines := 0
	for {
		r := l.peek()
		if !isWhitespace(r) {
			break
		}
		if r == '\n' {
			newlines++
		}
		l.advance()
	}
	start := l.Pos()
	tok, err := l.scan(start)
	if err != nil {
		if e, ok := err.(*Error); ok {
			e.Pos = l.Resolve(e.Pos)
		}
		return Token{}, err
	}
	tok.Pos = start
	tok.End = l.off
	tok.Raw = l.src[start.Offset:l.off]
	tok.Newlines = newlines
	return tok, nil
}

func (l *Lexer) scan(start Position) (Token, error) {
	r := l.peek()
	switch r {
	case -1:
		return Token{Kind: EOF}, nil
	case '(':
		l.advance()
		return Token{Kind: LParen}, nil
	case ')':
		l.advance()
		return Token{Kind: RParen}, nil
	case '[':
		l.advance()
		return Token{Kind: LBracket}, nil
	case ']':
		l.advance()
		return Token{Kind: RBracket}, nil
	case '{':
		l.advance()
		return Token{Kind: LBrace}, nil
	case '}':
		l.advance()
		return Token{Kind: RBrace}, nil
	case ',':
		l.advance()
		return Token{Kind: Comma}, nil
	case ':':
		l.advance()
		return Token{Kind: Colon}, nil
	case '#':
		l.advance()
		return Token{Kind: Hash}, nil
	case '!':
		l.advance()
		return Token{Kind: Bang}, nil
	case '/':
		switch l.peekAt(1) {
		case '/':
			for r := l.peek(); r != -1 && r != '\n'; r = l.peek() {
				l.advance()
			}
			return Token{Kind: LineComment}, nil
		case '*':
			return l.scanBlockComment(start)
		}
		return Token{}, Errorf(start, "unexpected character '/'")
	case '"':
		l.advance()
		s, err := l.scanQuoted(start, '"', false)
		return Token{Kind: String, Str: s}, err
	case '\'':
		l.advance()
		c, err := l.scanChar(start, false)
		return Token{Kind: Char, Rune: c}, err
	case 'b':
		switch l.peekAt(1) {
		case '"':
			l.advance()
			l.advance()
			s, err := l.scanQuoted(start, '"', true)
			return Token{Kind: ByteString, Str: s}, err
		case '\'':
			l.advance()
			l.advance()
			c, err := l.scanChar(start, true)
			return Token{Kind: Byte, Rune: c}, err
		case 'r':
			if n := l.peekAt(2); n == '"' || n == '#' {
				l.advance()
				l.advance()
				s, err := l.scanRaw(start)
				return Token{Kind: ByteString, Str: s}, err
			}
		}
	case 'r':
		switch l.peekAt(1) {
		case '"':
			l.advance()
			s, err := l.scanRaw(start)
			return Token{Kind: String, Str: s}, err
		case '#':
			// Either a raw string r#"..."# or a raw identifier r#name.
			i := 1
			for l.peekAt(i) == '#' {
				i++
			}
			if l.peekAt(i) == '"' {
				l.advance()
				s, err := l.scanRaw(start)
				return Token{Kind: String, Str: s}, err
			}
			if i == 2 && IsRawIdentContinue(l.peekAt(2)) {
				l.advance()
				l.advance()
				b := l.off
				for IsRawIdentContinue(l.peek()) {
					l.advance()
				}
				return Token{Kind: Ident, Str: l.src[b:l.off], RawIdent: true}, nil
			}
		}
	}
	if isDigit(r) || r == '+' || r == '-' || (r == '.' && isDigit(l.peekAt(1))) {
		return l.scanNumber(start)
	}
	if IsIdentStart(r) {
		b := l.off
		for IsIdentContinue(l.peek()) {
			l.advance()
		}
		return Token{Kind: Ident, Str: l.src[b:l.off]}, nil
	}
	if r == utf8.RuneError {
		return Token{}, Errorf(start, "invalid UTF-8 encoding")
	}
	return Token{}, Errorf(start, "unexpected character %q", r)
}

func (l *Lexer) scanBlockComment(start Position) (Token, error) {
	l.advance()
	l.advance()
	depth := 1
	for depth > 0 {
		switch l.peek() {
		case -1:
			return Token{}, Errorf(start, "unterminated block comment")
		case '/':
			l.advance()
			if l.peek() == '*' {
				l.advance()
				depth++
			}
		case '*':
			l.advance()
			if l.peek() == '/' {
				l.advance()
				depth--
			}
		default:
			l.advance()
		}
	}
	return Token{Kind: BlockComment}, nil
}

// scanRaw scans a raw string after the leading r (or br). The lexer is
// positioned at the first '#' or the opening quote.
func (l *Lexer) scanRaw(start Position) (string, error) {
	hashes := 0
	for l.peek() == '#' {
		l.advance()
		hashes++
	}
	if l.peek() != '"' {
		return "", Errorf(start, "expected '\"' to start raw string")
	}
	l.advance()
	closing := "\"" + strings.Repeat("#", hashes)
	idx := strings.Index(l.src[l.off:], closing)
	if idx < 0 {
		return "", Errorf(start, "unterminated raw string")
	}
	s := l.src[l.off : l.off+idx]
	for target := l.off + idx + len(closing); l.off < target; {
		l.advance()
	}
	return s, nil
}

// scanQuoted scans a quoted string after the opening quote and returns the
// decoded contents. For byte strings the result holds arbitrary bytes.
func (l *Lexer) scanQuoted(start Position, quote rune, byteStr bool) (string, error) {
	// Fast path: no escapes before the closing quote.
	if !byteStr {
		rest := l.src[l.off:]
		if i := strings.IndexAny(rest, `"\`); i >= 0 && rest[i] == '"' {
			s := rest[:i]
			for range s {
				// advance updates line and column; strings are rarely long
				// enough for this to matter.
				l.advance()
			}
			l.advance()
			return s, nil
		}
	}
	var sb strings.Builder
	for {
		p := l.Pos()
		r := l.peek()
		switch r {
		case -1:
			return "", Errorf(start, "unterminated string")
		case quote:
			l.advance()
			return sb.String(), nil
		case '\\':
			l.advance()
			if l.peek() == '\n' || l.peek() == '\r' {
				// Line continuation: skip the newline and leading whitespace.
				for isWhitespace(l.peek()) {
					l.advance()
				}
				continue
			}
			c, isByte, err := l.scanEscape(p, byteStr)
			if err != nil {
				return "", err
			}
			if isByte {
				sb.WriteByte(byte(c))
			} else {
				sb.WriteRune(c)
			}
		default:
			if r == utf8.RuneError {
				if _, size := utf8.DecodeRuneInString(l.src[l.off:]); size == 1 {
					return "", Errorf(p, "invalid UTF-8 encoding")
				}
			}
			if byteStr && r >= utf8.RuneSelf {
				return "", Errorf(p, "non-ASCII character in byte string")
			}
			l.advance()
			sb.WriteRune(r)
		}
	}
}

// scanChar scans a char or byte literal after the opening quote.
func (l *Lexer) scanChar(start Position, byteLit bool) (rune, error) {
	p := l.Pos()
	r := l.peek()
	var c rune
	switch r {
	case -1, '\'':
		return 0, Errorf(start, "empty or unterminated char literal")
	case '\\':
		l.advance()
		var err error
		c, _, err = l.scanEscape(p, byteLit)
		if err != nil {
			return 0, err
		}
	default:
		if byteLit && r >= utf8.RuneSelf {
			return 0, Errorf(p, "non-ASCII character in byte literal")
		}
		if r == utf8.RuneError {
			return 0, Errorf(p, "invalid UTF-8 encoding")
		}
		c = l.advance()
	}
	if l.peek() != '\'' {
		return 0, Errorf(start, "char literal must contain exactly one character")
	}
	l.advance()
	return c, nil
}

// scanEscape decodes an escape sequence after the backslash. isByte reports
// whether the result is a raw byte (from \x in a byte string).
func (l *Lexer) scanEscape(p Position, byteStr bool) (r rune, isByte bool, err error) {
	c := l.advance()
	switch c {
	case '"', '\'', '\\', '/':
		return c, false, nil
	case 'n':
		return '\n', false, nil
	case 'r':
		return '\r', false, nil
	case 't':
		return '\t', false, nil
	case 'b':
		return '\b', false, nil
	case 'f':
		return '\f', false, nil
	case '0':
		return 0, false, nil
	case 'x':
		h := l.takeHex(2)
		if len(h) != 2 {
			return 0, false, Errorf(p, "invalid \\x escape")
		}
		v, _ := strconv.ParseUint(h, 16, 8)
		if !byteStr && v > 0x7F {
			return 0, false, Errorf(p, "\\x escape out of range, must be at most \\x7F in strings")
		}
		return rune(v), byteStr, nil
	case 'u':
		if byteStr {
			return 0, false, Errorf(p, "unicode escape in byte string")
		}
		var h string
		if l.peek() == '{' {
			l.advance()
			h = l.takeHex(6)
			if l.peek() != '}' || h == "" {
				return 0, false, Errorf(p, "invalid \\u{...} escape")
			}
			l.advance()
		} else {
			h = l.takeHex(4)
			if len(h) != 4 {
				return 0, false, Errorf(p, "invalid \\u escape")
			}
		}
		v, _ := strconv.ParseUint(h, 16, 32)
		if v > unicode.MaxRune || (v >= 0xD800 && v <= 0xDFFF) {
			return 0, false, Errorf(p, "invalid unicode code point in escape")
		}
		return rune(v), false, nil
	case -1:
		return 0, false, Errorf(p, "unterminated escape sequence")
	}
	return 0, false, Errorf(p, "unknown escape sequence \\%c", c)
}

func (l *Lexer) takeHex(maxDigits int) string {
	b := l.off
	for i := range maxDigits {
		r := l.peek()
		isHex := isBaseDigit(r, 16) || (r == '_' && i > 0)
		if !isHex {
			break
		}
		l.advance()
	}
	return strings.ReplaceAll(l.src[b:l.off], "_", "")
}

// scanNumber scans integer and float literals including a leading sign,
// base prefixes, underscores, exponents, type suffixes, inf and NaN.
func (l *Lexer) scanNumber(start Position) (Token, error) {
	var sb strings.Builder
	if r := l.peek(); r == '+' || r == '-' {
		l.advance()
		if r == '-' {
			sb.WriteRune(r)
		}
		if IsIdentStart(l.peek()) {
			b := l.off
			for IsIdentContinue(l.peek()) {
				l.advance()
			}
			word := l.src[b:l.off]
			switch word {
			case "inf", "NaN":
				sb.WriteString(word)
				return Token{Kind: Float, Str: sb.String()}, nil
			case "inff32", "inff64", "NaNf32", "NaNf64":
				sb.WriteString(word[:len(word)-3])
				return Token{Kind: Float, Str: sb.String(), Suffix: word[len(word)-3:]}, nil
			}
			return Token{}, Errorf(start, "expected number after sign, found %q", word)
		}
		if startsFraction := l.peek() == '.' && isDigit(l.peekAt(1)); !isDigit(l.peek()) && !startsFraction {
			return Token{}, Errorf(start, "expected number after sign")
		}
	}

	// Base prefixed integers.
	if l.peek() == '0' {
		base := 0
		switch l.peekAt(1) {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			l.advance()
			l.advance()
			sb.WriteString(basePrefix[base])
			digits := 0
			for {
				r := l.peek()
				if r == '_' {
					l.advance()
					continue
				}
				if !isBaseDigit(r, base) {
					break
				}
				sb.WriteRune(l.advance())
				digits++
			}
			if digits == 0 {
				return Token{}, Errorf(start, "missing digits after base prefix")
			}
			suffix := l.scanSuffix()
			if suffix != "" && !intSuffixes[suffix] {
				return Token{}, Errorf(start, "invalid integer suffix %q", suffix)
			}
			if isDigit(l.peek()) {
				return Token{}, Errorf(start, "invalid digit for base %d literal", base)
			}
			return Token{Kind: Int, Str: sb.String(), Suffix: suffix}, nil
		}
	}

	isFloat := false
	digits := 0
	for r := l.peek(); isDigit(r) || r == '_'; r = l.peek() {
		l.advance()
		if r != '_' {
			sb.WriteRune(r)
			digits++
		}
	}
	if l.peek() == '.' && !IsIdentStart(l.peekAt(1)) {
		isFloat = true
		l.advance()
		sb.WriteByte('.')
		for r := l.peek(); isDigit(r) || (r == '_' && digits > 0); r = l.peek() {
			l.advance()
			if r != '_' {
				sb.WriteRune(r)
				digits++
			}
		}
	}
	if digits == 0 {
		return Token{}, Errorf(start, "invalid number")
	}
	if r := l.peek(); r == 'e' || r == 'E' {
		n := 1
		if s := l.peekAt(1); s == '+' || s == '-' {
			n = 2
		}
		if isDigit(l.peekAt(n)) || (l.peekAt(n) == '_' && isDigit(l.peekAt(n+1))) {
			isFloat = true
			sb.WriteByte('e')
			l.advance()
			if n == 2 {
				sb.WriteRune(l.advance())
			}
			for r := l.peek(); isDigit(r) || r == '_'; r = l.peek() {
				l.advance()
				if r != '_' {
					sb.WriteRune(r)
				}
			}
		}
	}
	suffix := l.scanSuffix()
	switch {
	case suffix == "":
	case suffix == "f32" || suffix == "f64":
		isFloat = true
	case intSuffixes[suffix]:
		if isFloat {
			return Token{}, Errorf(start, "integer suffix %q on float literal", suffix)
		}
	default:
		return Token{}, Errorf(start, "invalid number suffix %q", suffix)
	}
	if isFloat {
		return Token{Kind: Float, Str: sb.String(), Suffix: suffix}, nil
	}
	return Token{Kind: Int, Str: sb.String(), Suffix: suffix}, nil
}

func (l *Lexer) scanSuffix() string {
	if !IsIdentStart(l.peek()) {
		return ""
	}
	b := l.off
	for IsIdentContinue(l.peek()) {
		l.advance()
	}
	return l.src[b:l.off]
}

var basePrefix = map[int]string{16: "0x", 8: "0o", 2: "0b"}

func isBaseDigit(r rune, base int) bool {
	switch base {
	case 2:
		return r == '0' || r == '1'
	case 8:
		return r >= '0' && r <= '7'
	case 16:
		return isDigit(r) || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
	}
	return false
}
