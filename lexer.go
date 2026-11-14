// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokenKind uint8

const (
	tokEOF tokenKind = iota
	tokLParen
	tokRParen
	tokLBracket
	tokRBracket
	tokLBrace
	tokRBrace
	tokComma
	tokColon
	tokHash
	tokBang
	tokIdent
	tokInt
	tokFloat
	tokString
	tokByteString
	tokChar
	tokByte
	tokLineComment
	tokBlockComment
)

var tokenNames = [...]string{
	tokEOF:          "end of input",
	tokLParen:       "'('",
	tokRParen:       "')'",
	tokLBracket:     "'['",
	tokRBracket:     "']'",
	tokLBrace:       "'{'",
	tokRBrace:       "'}'",
	tokComma:        "','",
	tokColon:        "':'",
	tokHash:         "'#'",
	tokBang:         "'!'",
	tokIdent:        "identifier",
	tokInt:          "integer",
	tokFloat:        "float",
	tokString:       "string",
	tokByteString:   "byte string",
	tokChar:         "char",
	tokByte:         "byte literal",
	tokLineComment:  "comment",
	tokBlockComment: "comment",
}

func (k tokenKind) String() string { return tokenNames[k] }

type token struct {
	kind tokenKind
	pos  Position
	end  int    // byte offset just past the token
	raw  string // the token's source text

	// Decoded payload. For identifiers this is the name without any r#
	// prefix, for numbers the literal without underscores and suffix, for
	// strings and byte strings the decoded contents.
	str    string
	suffix string // numeric type suffix such as "u8" or "f32"
	r      rune   // value of a char or byte literal
	rawID  bool   // identifier was written as r#name

	newlines int // newlines in the whitespace before the token
}

type lexer struct {
	src  string
	off  int
	line int
	col  int
}

func newLexer(src []byte) *lexer {
	l := &lexer{src: string(src), line: 1, col: 1}
	// Skip a UTF-8 byte order mark.
	if strings.HasPrefix(l.src, "\xEF\xBB\xBF") {
		l.off = 3
	}
	return l
}

func (l *lexer) pos() Position { return Position{Offset: l.off, Line: l.line, Column: l.col} }

func (l *lexer) errorf(p Position, format string, args ...any) error {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	return &SyntaxError{Msg: msg, Pos: p}
}

// peek returns the rune at off+n bytes, or -1 at end of input.
func (l *lexer) peekAt(n int) rune {
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

func (l *lexer) peek() rune { return l.peekAt(0) }

// advance consumes one rune and returns it.
func (l *lexer) advance() rune {
	if l.off >= len(l.src) {
		return -1
	}
	r, size := rune(l.src[l.off]), 1
	if r >= utf8.RuneSelf {
		r, size = utf8.DecodeRuneInString(l.src[l.off:])
	}
	l.off += size
	if r == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return r
}

func isWhitespace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f', 0x85, 0x200E, 0x200F, 0x2028, 0x2029:
		return true
	}
	return false
}

func isIdentStart(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= utf8.RuneSelf && unicode.IsLetter(r))
}

func isIdentContinue(r rune) bool {
	return isIdentStart(r) || (r >= '0' && r <= '9') || (r >= utf8.RuneSelf && (unicode.IsDigit(r) || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Mc, r) || unicode.Is(unicode.Pc, r)))
}

func isRawIdentContinue(r rune) bool {
	return isIdentContinue(r) || r == '.' || r == '+' || r == '-'
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

// next returns the next token, including comments.
func (l *lexer) next() (token, error) {
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
	start := l.pos()
	tok, err := l.scan(start)
	if err != nil {
		return token{}, err
	}
	tok.pos = start
	tok.end = l.off
	tok.raw = l.src[start.Offset:l.off]
	tok.newlines = newlines
	return tok, nil
}

func (l *lexer) scan(start Position) (token, error) {
	r := l.peek()
	switch r {
	case -1:
		return token{kind: tokEOF}, nil
	case '(':
		l.advance()
		return token{kind: tokLParen}, nil
	case ')':
		l.advance()
		return token{kind: tokRParen}, nil
	case '[':
		l.advance()
		return token{kind: tokLBracket}, nil
	case ']':
		l.advance()
		return token{kind: tokRBracket}, nil
	case '{':
		l.advance()
		return token{kind: tokLBrace}, nil
	case '}':
		l.advance()
		return token{kind: tokRBrace}, nil
	case ',':
		l.advance()
		return token{kind: tokComma}, nil
	case ':':
		l.advance()
		return token{kind: tokColon}, nil
	case '#':
		l.advance()
		return token{kind: tokHash}, nil
	case '!':
		l.advance()
		return token{kind: tokBang}, nil
	case '/':
		switch l.peekAt(1) {
		case '/':
			for r := l.peek(); r != -1 && r != '\n'; r = l.peek() {
				l.advance()
			}
			return token{kind: tokLineComment}, nil
		case '*':
			return l.scanBlockComment(start)
		}
		return token{}, l.errorf(start, "unexpected character '/'")
	case '"':
		l.advance()
		s, err := l.scanQuoted(start, '"', false)
		return token{kind: tokString, str: s}, err
	case '\'':
		l.advance()
		c, err := l.scanChar(start, false)
		return token{kind: tokChar, r: c}, err
	case 'b':
		switch l.peekAt(1) {
		case '"':
			l.advance()
			l.advance()
			s, err := l.scanQuoted(start, '"', true)
			return token{kind: tokByteString, str: s}, err
		case '\'':
			l.advance()
			l.advance()
			c, err := l.scanChar(start, true)
			return token{kind: tokByte, r: c}, err
		case 'r':
			if n := l.peekAt(2); n == '"' || n == '#' {
				l.advance()
				l.advance()
				s, err := l.scanRaw(start)
				return token{kind: tokByteString, str: s}, err
			}
		}
	case 'r':
		switch l.peekAt(1) {
		case '"':
			l.advance()
			s, err := l.scanRaw(start)
			return token{kind: tokString, str: s}, err
		case '#':
			// Either a raw string r#"..."# or a raw identifier r#name.
			i := 1
			for l.peekAt(i) == '#' {
				i++
			}
			if l.peekAt(i) == '"' {
				l.advance()
				s, err := l.scanRaw(start)
				return token{kind: tokString, str: s}, err
			}
			if i == 2 && isRawIdentContinue(l.peekAt(2)) {
				l.advance()
				l.advance()
				b := l.off
				for isRawIdentContinue(l.peek()) {
					l.advance()
				}
				return token{kind: tokIdent, str: l.src[b:l.off], rawID: true}, nil
			}
		}
	}
	if isDigit(r) || r == '+' || r == '-' || (r == '.' && isDigit(l.peekAt(1))) {
		return l.scanNumber(start)
	}
	if isIdentStart(r) {
		b := l.off
		for isIdentContinue(l.peek()) {
			l.advance()
		}
		return token{kind: tokIdent, str: l.src[b:l.off]}, nil
	}
	if r == utf8.RuneError {
		return token{}, l.errorf(start, "invalid UTF-8 encoding")
	}
	return token{}, l.errorf(start, "unexpected character %q", r)
}

func (l *lexer) scanBlockComment(start Position) (token, error) {
	l.advance()
	l.advance()
	depth := 1
	for depth > 0 {
		switch l.peek() {
		case -1:
			return token{}, l.errorf(start, "unterminated block comment")
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
	return token{kind: tokBlockComment}, nil
}

// scanRaw scans a raw string after the leading r (or br). The lexer is
// positioned at the first '#' or the opening quote.
func (l *lexer) scanRaw(start Position) (string, error) {
	hashes := 0
	for l.peek() == '#' {
		l.advance()
		hashes++
	}
	if l.peek() != '"' {
		return "", l.errorf(start, "expected '\"' to start raw string")
	}
	l.advance()
	closing := "\"" + strings.Repeat("#", hashes)
	idx := strings.Index(l.src[l.off:], closing)
	if idx < 0 {
		return "", l.errorf(start, "unterminated raw string")
	}
	s := l.src[l.off : l.off+idx]
	for target := l.off + idx + len(closing); l.off < target; {
		l.advance()
	}
	return s, nil
}

// scanQuoted scans a quoted string after the opening quote and returns the
// decoded contents. For byte strings the result holds arbitrary bytes.
func (l *lexer) scanQuoted(start Position, quote rune, byteStr bool) (string, error) {
	var sb strings.Builder
	for {
		p := l.pos()
		r := l.peek()
		switch {
		case r == -1:
			return "", l.errorf(start, "unterminated string")
		case r == quote:
			l.advance()
			return sb.String(), nil
		case r == '\\':
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
					return "", l.errorf(p, "invalid UTF-8 encoding")
				}
			}
			if byteStr && r >= utf8.RuneSelf {
				return "", l.errorf(p, "non-ASCII character in byte string")
			}
			l.advance()
			sb.WriteRune(r)
		}
	}
}

// scanChar scans a char or byte literal after the opening quote.
func (l *lexer) scanChar(start Position, byteLit bool) (rune, error) {
	p := l.pos()
	r := l.peek()
	var c rune
	switch r {
	case -1, '\'':
		return 0, l.errorf(start, "empty or unterminated char literal")
	case '\\':
		l.advance()
		var err error
		c, _, err = l.scanEscape(p, byteLit)
		if err != nil {
			return 0, err
		}
	default:
		if byteLit && r >= utf8.RuneSelf {
			return 0, l.errorf(p, "non-ASCII character in byte literal")
		}
		if r == utf8.RuneError {
			return 0, l.errorf(p, "invalid UTF-8 encoding")
		}
		c = l.advance()
	}
	if l.peek() != '\'' {
		return 0, l.errorf(start, "char literal must contain exactly one character")
	}
	l.advance()
	return c, nil
}

// scanEscape decodes an escape sequence after the backslash. isByte reports
// whether the result is a raw byte (from \x in a byte string).
func (l *lexer) scanEscape(p Position, byteStr bool) (r rune, isByte bool, err error) {
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
			return 0, false, l.errorf(p, "invalid \\x escape")
		}
		v, _ := strconv.ParseUint(h, 16, 8)
		if !byteStr && v > 0x7F {
			return 0, false, l.errorf(p, "\\x escape out of range, must be at most \\x7F in strings")
		}
		return rune(v), byteStr, nil
	case 'u':
		if byteStr {
			return 0, false, l.errorf(p, "unicode escape in byte string")
		}
		var h string
		if l.peek() == '{' {
			l.advance()
			h = l.takeHex(6)
			if l.peek() != '}' || h == "" {
				return 0, false, l.errorf(p, "invalid \\u{...} escape")
			}
			l.advance()
		} else {
			h = l.takeHex(4)
			if len(h) != 4 {
				return 0, false, l.errorf(p, "invalid \\u escape")
			}
		}
		v, _ := strconv.ParseUint(h, 16, 32)
		if v > unicode.MaxRune || (v >= 0xD800 && v <= 0xDFFF) {
			return 0, false, l.errorf(p, "invalid unicode code point in escape")
		}
		return rune(v), false, nil
	case -1:
		return 0, false, l.errorf(p, "unterminated escape sequence")
	}
	return 0, false, l.errorf(p, "unknown escape sequence \\%c", c)
}

func (l *lexer) takeHex(maxDigits int) string {
	b := l.off
	for i := range maxDigits {
		r := l.peek()
		if !(isDigit(r) || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') || (r == '_' && i > 0)) {
			break
		}
		l.advance()
	}
	return strings.ReplaceAll(l.src[b:l.off], "_", "")
}

var intSuffixes = map[string]bool{
	"i8": true, "i16": true, "i32": true, "i64": true, "i128": true, "isize": true,
	"u8": true, "u16": true, "u32": true, "u64": true, "u128": true, "usize": true,
}

// scanNumber scans integer and float literals including a leading sign,
// base prefixes, underscores, exponents, type suffixes, inf and NaN.
func (l *lexer) scanNumber(start Position) (token, error) {
	var sb strings.Builder
	if r := l.peek(); r == '+' || r == '-' {
		l.advance()
		if r == '-' {
			sb.WriteRune(r)
		}
		if isIdentStart(l.peek()) {
			b := l.off
			for isIdentContinue(l.peek()) {
				l.advance()
			}
			word := l.src[b:l.off]
			switch word {
			case "inf", "NaN":
				sb.WriteString(word)
				return token{kind: tokFloat, str: sb.String()}, nil
			case "inff32", "inff64", "NaNf32", "NaNf64":
				sb.WriteString(word[:len(word)-3])
				return token{kind: tokFloat, str: sb.String(), suffix: word[len(word)-3:]}, nil
			}
			return token{}, l.errorf(start, "expected number after sign, found %q", word)
		}
		if !isDigit(l.peek()) && !(l.peek() == '.' && isDigit(l.peekAt(1))) {
			return token{}, l.errorf(start, "expected number after sign")
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
				return token{}, l.errorf(start, "missing digits after base prefix")
			}
			suffix := l.scanSuffix()
			if suffix != "" && !intSuffixes[suffix] {
				return token{}, l.errorf(start, "invalid integer suffix %q", suffix)
			}
			if isDigit(l.peek()) {
				return token{}, l.errorf(start, "invalid digit for base %d literal", base)
			}
			return token{kind: tokInt, str: sb.String(), suffix: suffix}, nil
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
	if l.peek() == '.' && !isIdentStart(l.peekAt(1)) {
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
		return token{}, l.errorf(start, "invalid number")
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
			return token{}, l.errorf(start, "integer suffix %q on float literal", suffix)
		}
	default:
		return token{}, l.errorf(start, "invalid number suffix %q", suffix)
	}
	if isFloat {
		return token{kind: tokFloat, str: sb.String(), suffix: suffix}, nil
	}
	return token{kind: tokInt, str: sb.String(), suffix: suffix}, nil
}

func (l *lexer) scanSuffix() string {
	if !isIdentStart(l.peek()) {
		return ""
	}
	b := l.off
	for isIdentContinue(l.peek()) {
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
