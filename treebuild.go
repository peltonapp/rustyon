// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/peltonapp/rustyon-go/internal/syntax"
)

// builder builds the syntax tree. It is a transliteration of the value
// parser in parser.go: every decision about what is valid is made by the
// same parser methods, and the code here only records the structure and the
// comments around it. Keep the two in step.
//
// Comments live in the whitespace between tokens. ws skips whitespace with
// the parser's own skipWS and then records the comments found in the skipped
// region into pend. The consumer of the next token takes them.
type builder struct {
	p     *parser
	pend  []fcomment // comments since the last token
	nl    int        // newlines in the whitespace right before the next token
	stray []fcomment // comments in unusual places, emitted before the enclosing item
}

type collKind uint8

const (
	kList collKind = iota
	kMap
	kTuple
	kStruct
)

// commentEnd returns the offset just past the comment starting at i.
func commentEnd(src string, i int) int {
	if src[i+1] == '/' {
		j := strings.IndexByte(src[i:], '\n')
		if j < 0 {
			return len(src)
		}
		return i + j
	}
	depth := 1
	j := i + 2
	for depth > 0 && j < len(src) {
		switch {
		case src[j] == '/' && j+1 < len(src) && src[j+1] == '*':
			depth++
			j += 2
		case src[j] == '*' && j+1 < len(src) && src[j+1] == '/':
			depth--
			j += 2
		default:
			j++
		}
	}
	return j
}

func newComment(src string, i, j, newlines int) fcomment {
	text := src[i:j]
	block := text[1] == '*'
	if !block {
		text = strings.TrimRight(text, " \t\r")
	}
	return fcomment{text: text, off: int32(i), block: block, newlines: newlines}
}

// ws skips whitespace and comments and records the comments.
func (b *builder) ws() error {
	p := b.p
	start := p.cur
	if err := p.skipWS(); err != nil {
		return err
	}
	if p.cur == start {
		return nil
	}
	src := p.src
	nl := 0
	for i := start; i < p.cur; {
		switch src[i] {
		case '\n':
			nl++
			i++
		case '/':
			j := commentEnd(src, i)
			b.pend = append(b.pend, newComment(src, i, j, nl))
			nl = 0
			i = j
		default:
			i++
		}
	}
	b.nl = nl
	return nil
}

// take returns the pending comments and the newlines before the next token
// and clears both.
func (b *builder) take() ([]fcomment, uint8) {
	cs, nl := b.pend, capNewlines(b.nl)
	b.pend, b.nl = nil, 0
	return cs, nl
}

// sameLine removes and returns the pending comments that start on the line
// of the previous token.
func (b *builder) sameLine() []fcomment {
	i := 0
	for i < len(b.pend) && b.pend[i].newlines == 0 {
		i++
	}
	if i == 0 {
		return nil
	}
	out := b.pend[:i:i]
	b.pend = b.pend[i:]
	if len(b.pend) == 0 {
		b.pend = nil
	}
	return out
}

// strayAll moves the pending comments to the stray list.
func (b *builder) strayAll() {
	for _, c := range b.pend {
		c.newlines = 1
		b.stray = append(b.stray, c)
	}
	b.pend = nil
	b.nl = 0
}

func (b *builder) atom(start int) fnode {
	return fnode{text: b.p.src[start:b.p.cur], start: int32(start), end: int32(b.p.cur)}
}

func isTerminator(src string, i int) bool {
	if i >= len(src) {
		return true
	}
	switch src[i] {
	case ',', ')', ']', '}', ':', ' ', '\t', '\n', '\r':
		return true
	}
	return false
}

// fastNumber accepts the plain decimal integers and floats that make up most
// documents. Everything it accepts is accepted by the full number parser
// too; anything else is left to it.
func (p *parser) fastNumber() bool {
	src := p.src
	i := p.cur
	if src[i] == '-' {
		i++
	}
	d0 := i
	for i < len(src) && src[i] >= '0' && src[i] <= '9' {
		i++
	}
	if i == d0 {
		return false
	}
	if i < len(src) && src[i] == '.' {
		j := i + 1
		for j < len(src) && src[j] >= '0' && src[j] <= '9' {
			j++
		}
		if j == i+1 {
			return false
		}
		i = j
	}
	if i-d0 > 18 || !isTerminator(src, i) {
		return false
	}
	p.cur = i
	return true
}

// guarded mirrors parser.guarded.
func (b *builder) guarded() (fnode, error) {
	p := b.p
	if p.limit == 0 {
		return fnode{}, p.errRecursion()
	}
	p.limit--
	n, err := b.value()
	p.limit++
	return n, err
}

// value mirrors parser.parseValue.
func (b *builder) value() (fnode, error) {
	p := b.p
	start := p.cur
	if start < len(p.src) {
		switch c := p.src[start]; {
		case c == '"':
			rest := p.src[start+1:]
			if i := strings.IndexAny(rest, "\"\\"); i >= 0 && rest[i] == '"' {
				p.cur = start + 1 + i + 1
				return b.atom(start), nil
			}
		case (c >= '0' && c <= '9') || c == '-':
			if p.fastNumber() {
				return b.atom(start), nil
			}
		}
	}
	switch {
	case p.consumeIdent("true"), p.consumeIdent("false"), p.consumeIdent("None"):
		return b.atom(start), nil
	case p.checkIdent("Some"):
		return b.option()
	case p.consumeStr("()"):
		return fnode{
			start: int32(start), end: int32(p.cur),
			c: &fcoll{open: '(', close: ')', o: int32(start), cl: int32(start) + 1},
		}, nil
	case p.consumeIdent("inf"), p.consumeIdent("inff32"), p.consumeIdent("inff64"):
		if _, err := p.floatRangeOrValue(number{isFloat: true, f: math.Inf(1)}); err != nil {
			return fnode{}, err
		}
		return b.atom(start), nil
	case p.consumeIdent("NaN"), p.consumeIdent("NaNf32"), p.consumeIdent("NaNf64"):
		if _, err := p.floatRangeOrValue(number{isFloat: true, f: math.NaN()}); err != nil {
			return fnode{}, err
		}
		return b.atom(start), nil
	}
	if _, ok := p.skipIdentifier(); ok {
		name := b.atom(start)
		save, pend, nl := p.cur, len(b.pend), b.nl
		if err := b.ws(); err != nil {
			return fnode{}, err
		}
		st, err := p.checkStructType(true, false)
		if err != nil {
			return fnode{}, err
		}
		if st == stUnit {
			// A bare name: the whitespace after it belongs to what follows.
			p.cur, b.pend, b.nl = save, b.pend[:pend], nl
			return name, nil
		}
		b.strayAll()
		return b.paren(name, st)
	}
	c, err := p.peekRuneOrEOF()
	if err != nil {
		return fnode{}, err
	}
	switch {
	case c == '(':
		st, err := p.checkStructType(true, false)
		if err != nil {
			return fnode{}, err
		}
		return b.paren(fnode{start: int32(start)}, st)
	case c == '[':
		return b.coll(fnode{start: int32(start)}, kList)
	case c == '{':
		return b.coll(fnode{start: int32(start)}, kMap)
	case (c >= '0' && c <= '9') || c == '+' || c == '-' || c == '.' || c == 'b':
		if c == 'b' && !p.checkStr("b'") {
			if _, err := p.parseByteStringNoBase64(); err != nil {
				return fnode{}, err
			}
			return b.atom(start), nil
		}
		if p.consumeStr("..=") {
			if _, err := p.anyNumber(); err != nil {
				return fnode{}, err
			}
			return b.atom(start), nil
		}
		if p.consumeStr("..") {
			if p.checkAnyNumberStart() {
				if _, err := p.anyNumber(); err != nil {
					return fnode{}, err
				}
			}
			return b.atom(start), nil
		}
		num, err := p.anyNumber()
		if err != nil {
			return fnode{}, err
		}
		if _, err := p.floatRangeOrValue(num); err != nil {
			return fnode{}, err
		}
		return b.atom(start), nil
	case c == '"' || c == 'r':
		if _, err := p.parseStringLit(); err != nil {
			return fnode{}, err
		}
		return b.atom(start), nil
	case c == '\'':
		if _, err := p.parseChar(); err != nil {
			return fnode{}, err
		}
		return b.atom(start), nil
	}
	return fnode{}, p.fail(errOther, "unexpected character "+strconv.QuoteRune(c))
}

func (b *builder) paren(name fnode, st structType) (fnode, error) {
	if st == stNamed {
		return b.coll(name, kStruct)
	}
	return b.coll(name, kTuple)
}

// coll builds a delimited collection. The parser is at the opening
// delimiter.
func (b *builder) coll(name fnode, kind collKind) (fnode, error) {
	p := b.p
	n := &fcoll{open: '(', close: ')', o: int32(p.cur)}
	msg := "expected ')' to end tuple"
	switch kind {
	case kList:
		n.open, n.close, msg = '[', ']', "expected ']' to end list"
	case kMap:
		n.open, n.close, msg = '{', '}', "expected '}' to end map"
	case kStruct:
		msg = "expected ')' to end struct"
	}
	p.cur++
	b.take()
	if err := p.enter(); err != nil {
		return fnode{}, err
	}
	if err := b.openBody(n); err != nil {
		return fnode{}, err
	}
	if err := b.items(n, kind); err != nil {
		return fnode{}, err
	}
	p.leave()
	if !p.consumeChar(n.close) {
		return fnode{}, p.fail(errOther, msg)
	}
	layout(n)
	name.c = n
	name.end = n.cl + 1
	return name, nil
}

// openBody records the comments on the line of the opening delimiter.
func (b *builder) openBody(n *fcoll) error {
	if err := b.ws(); err != nil {
		return err
	}
	n.afterOpen = b.sameLine()
	if len(n.afterOpen) > 0 {
		n.multiline = true
	}
	return nil
}

// items mirrors the element loops of the parser. It stops at the closing
// delimiter without consuming it.
func (b *builder) items(n *fcoll, kind collKind) error {
	p := b.p
	hadComma := true
	for {
		if err := b.ws(); err != nil {
			return err
		}
		ok, err := p.hasElement(hadComma, n.close)
		if err != nil {
			return err
		}
		if !ok {
			b.endColl(n)
			return nil
		}
		if kind == kStruct && p.limit == 0 {
			return p.errRecursion()
		}
		hadComma, err = b.item(n, kind, false)
		if err != nil {
			return err
		}
	}
}

// endColl records what precedes the closing delimiter.
func (b *builder) endColl(n *fcoll) {
	p := b.p
	n.tail = b.pend
	if len(n.tail) > 0 {
		n.multiline = true
	}
	n.spaced = len(n.items) == 0 && p.cur != int(n.o)+1
	n.closeN = capNewlines(b.nl)
	if b.nl > 0 {
		n.multiline = true
	}
	b.pend, b.nl = nil, 0
	n.cl = int32(p.cur)
}

// colon consumes the colon after a map key or a field name.
func (b *builder) colon(n *fcoll, msg string) error {
	p := b.p
	if err := b.ws(); err != nil {
		return err
	}
	b.strayAll()
	nlc := b.nl
	b.nl = 0
	if !p.consumeChar(':') {
		return p.fail(errOther, msg)
	}
	if nlc > 0 {
		n.multiline = true
	}
	if err := b.ws(); err != nil {
		return err
	}
	b.strayAll()
	if b.nl > 0 {
		n.multiline = true
	}
	b.nl = 0
	return nil
}

// item parses one element. With direct set the value is parsed without
// charging the recursion budget again.
func (b *builder) item(n *fcoll, kind collKind, direct bool) (bool, error) {
	p := b.p
	n.items = append(n.items, fitem{comma: -1})
	it := &n.items[len(n.items)-1]
	lead, nl := b.take()
	it.newlines = nl
	it.start = int32(p.cur)
	if len(lead) > 0 {
		it.comments().leading = lead
		n.multiline = true
	}
	if nl > 0 {
		n.multiline = true
	}
	switch kind {
	case kMap:
		k, err := b.guarded()
		if err != nil {
			return false, err
		}
		it.key, it.hasKey = k, true
		if err := b.colon(n, "expected ':' after map key"); err != nil {
			return false, err
		}
	case kStruct:
		start := p.cur
		if _, err := p.identifier(); err != nil {
			return false, err
		}
		it.key, it.isField = b.atom(start), true
		if err := b.colon(n, "expected ':' after field name"); err != nil {
			return false, err
		}
	}
	var v fnode
	var err error
	if direct {
		v, err = b.value()
	} else {
		v, err = b.guarded()
	}
	if err != nil {
		return false, err
	}
	it.value = v
	if len(b.stray) > 0 {
		cm := it.comments()
		cm.leading = append(cm.leading, b.stray...)
		b.stray = nil
		n.multiline = true
	}
	return b.afterValue(n, it)
}

// afterValue mirrors parser.comma and attaches the comments around the
// comma to the item.
func (b *builder) afterValue(n *fcoll, it *fitem) (bool, error) {
	p := b.p
	if err := b.ws(); err != nil {
		return false, err
	}
	var trailing []fcomment
	hadComma := false
	if p.checkChar(',') {
		hadComma = true
		it.comma = int32(p.cur)
		p.cur++
		if b.nl > 0 {
			n.multiline = true
		}
		before, _ := b.take()
		if err := b.ws(); err != nil {
			return false, err
		}
		trailing = append(before, b.sameLine()...)
	} else {
		trailing = b.sameLine()
	}
	if len(trailing) > 0 {
		it.comments().trailing = trailing
		n.multiline = true
	}
	return hadComma, nil
}

// option mirrors parser.parseOption.
func (b *builder) option() (fnode, error) {
	p := b.p
	start := p.cur
	p.consumeIdent("Some")
	node := fnode{text: p.src[start:p.cur], start: int32(start)}
	if err := b.ws(); err != nil {
		return fnode{}, err
	}
	if p.checkChar('(') {
		b.strayAll()
		return b.someParens(node)
	}
	if p.exts&ImplicitSome != 0 {
		b.strayAll()
		v, err := b.guarded()
		if err != nil {
			return fnode{}, err
		}
		node.c = &fcoll{items: []fitem{{value: v, comma: -1}}, breaks: v.breaks()}
		node.end = v.end
		return node, nil
	}
	return fnode{}, p.fail(errOther, "expected '(' after Some")
}

func (b *builder) someParens(node fnode) (fnode, error) {
	p := b.p
	n := &fcoll{open: '(', close: ')', o: int32(p.cur)}
	p.cur++
	b.take()
	if err := b.openBody(n); err != nil {
		return fnode{}, err
	}
	if p.limit == 0 {
		return fnode{}, p.errRecursion()
	}
	p.limit--
	err := b.someInner(n, p.exts&UnwrapVariantNewtypes != 0)
	p.limit++
	if err != nil {
		return fnode{}, err
	}
	if !p.consumeChar(')') {
		return fnode{}, p.fail(errOther, "expected ')' to end Some")
	}
	layout(n)
	node.c = n
	node.end = n.cl + 1
	return node, nil
}

// someInner parses the inside of Some( ... ). With unwrap_variant_newtypes
// the content can be a struct or tuple without its own parentheses.
func (b *builder) someInner(n *fcoll, newtype bool) error {
	p := b.p
	if newtype {
		if p.checkChar(')') {
			b.endColl(n)
			return nil
		}
		st, err := p.checkStructType(false, true)
		if err != nil {
			return err
		}
		kind := kList
		switch st {
		case stNamed:
			kind = kStruct
		case stEmptyTuple, stNonNewtypeTuple:
			kind = kTuple
		}
		if kind != kList {
			if err := p.enter(); err != nil {
				return err
			}
			if err := b.items(n, kind); err != nil {
				return err
			}
			p.leave()
			return nil
		}
	}
	if _, err := b.item(n, kList, true); err != nil {
		return err
	}
	if !p.checkChar(')') {
		return p.fail(errOther, "expected ')' to end Some")
	}
	b.endColl(n)
	return nil
}

// layout decides how a collection is printed, bottom up.
func layout(n *fcoll) {
	if n.open == '(' && len(n.items) == 1 && len(n.tail) == 0 && len(n.afterOpen) == 0 {
		it := &n.items[0]
		if !it.isField && it.cm == nil && it.value.c != nil && it.newlines == 0 && n.closeN == 0 {
			n.hug = true
		}
	}
	if !n.hug {
		n.expanded = n.multiline || len(n.tail) > 0 || len(n.afterOpen) > 0
		for i := range n.items {
			it := &n.items[i]
			if it.cm != nil || it.value.breaks() || it.key.breaks() {
				n.expanded = true
			}
		}
		n.breaks = n.expanded
	} else {
		n.breaks = n.items[0].value.breaks()
	}
}

// attrText normalizes the attribute at src[start:end], which the parser has
// accepted, and returns the comments written inside it.
func (b *builder) attrText(start, end int) (string, []fcomment, int) {
	src := b.p.src
	attrEnd := end
	var words []string
	var str string
	var cms []fcomment
	for i := start; i < end; {
		c := src[i]
		switch {
		case c == '/':
			j := commentEnd(src, i)
			cms = append(cms, newComment(src, i, j, 1))
			i = j
		case c == '"':
			j := i + 1
			for j < end && src[j] != '"' {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			j++
			str = src[i:min(j, end)]
			i = j
		case c == 'r' && i+1 < end && (src[i+1] == '"' || src[i+1] == '#'):
			h := 0
			for i+1+h < end && src[i+1+h] == '#' {
				h++
			}
			if i+1+h < end && src[i+1+h] == '"' {
				closing := "\"" + strings.Repeat("#", h)
				k := strings.Index(src[i+2+h:end], closing)
				j := min(i+2+h+k+len(closing), end)
				str = src[i:j]
				i = j
				continue
			}
			j := i + 2
			for j < end && isIdentRune(src, j) {
				j += runeLenAt(src, j)
			}
			words = append(words, src[i:j])
			i = j
		case c >= utf8.RuneSelf || c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			j := i
			for j < end && isIdentRune(src, j) {
				j += runeLenAt(src, j)
			}
			if j == i {
				j += runeLenAt(src, j)
			}
			words = append(words, src[i:j])
			i = j
		case c == ']':
			// The parser skips whitespace after the attribute; leave that
			// to the caller.
			attrEnd = i + 1
			i = end
		default:
			i++
		}
	}
	if len(words) > 0 && words[0] == "enable" {
		return "#![enable(" + joinNames(words[1:]) + ")]", cms, attrEnd
	}
	if len(words) == 0 {
		return src[start:attrEnd], cms, attrEnd
	}
	return "#![" + words[0] + " = " + str + "]", cms, attrEnd
}

func runeLenAt(s string, i int) int {
	_, n := utf8.DecodeRuneInString(s[i:])
	return n
}

func isIdentRune(s string, i int) bool {
	r, _ := utf8.DecodeRuneInString(s[i:])
	return syntax.IsXIDContinue(r)
}

// buildTree parses src into a syntax tree. It accepts exactly the documents
// ParseDocument accepts.
func buildTree(src []byte) (*fdoc, *parser, error) {
	p, err := newParser(src)
	if err != nil {
		return nil, nil, err
	}
	d, err := (&builder{p: p}).doc()
	if err != nil {
		return nil, nil, p.wrap(err)
	}
	return d, p, nil
}

func (b *builder) doc() (*fdoc, error) {
	p := b.p
	d := &fdoc{}
	if err := b.ws(); err != nil {
		return nil, err
	}
	for {
		for i := range b.pend {
			c := b.pend[i]
			d.header = append(d.header, fheader{comment: &c, newlines: c.newlines})
		}
		b.pend = nil
		if !p.checkChar('#') {
			break
		}
		nl := int(capNewlines(b.nl))
		start := p.cur
		ext, _, err := p.attribute()
		if err != nil {
			return nil, err
		}
		p.exts |= ext
		d.exts |= ext
		text, cms, attrEnd := b.attrText(start, p.cur)
		p.cur = attrEnd
		d.header = append(d.header, fheader{attr: text, newlines: nl})
		for i := range cms {
			d.header = append(d.header, fheader{comment: &cms[i], newlines: 1})
		}
		b.nl = 0
		if err := b.ws(); err != nil {
			return nil, err
		}
	}
	d.valueN = int(capNewlines(b.nl))
	b.nl = 0
	v, err := b.value()
	if err != nil {
		return nil, err
	}
	d.value = v
	for i := range b.stray {
		c := b.stray[i]
		d.header = append(d.header, fheader{comment: &c, newlines: 1})
	}
	b.stray = nil
	if err := b.ws(); err != nil {
		return nil, err
	}
	d.trailing = b.sameLine()
	d.after = b.pend
	if p.cur < len(p.src) {
		return nil, p.fail(errOther, "unexpected trailing characters")
	}
	return d, nil
}
