// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package syntax

import (
	"bytes"
	"strings"
)

// Format reformats a RON document while preserving comments. See the
// rustyon.Format documentation for the layout rules.
func Format(src []byte, indent string) ([]byte, error) {
	if err := CheckUTF8(src); err != nil {
		return nil, err
	}
	p, err := newFParser(src)
	if err != nil {
		return nil, err
	}
	doc, err := p.parseDoc()
	if err != nil {
		return nil, err
	}
	f := &formatter{indent: indent}
	f.sb.Grow(len(src) + len(src)/2)
	f.doc(doc)
	return f.sb.Bytes(), nil
}

// The formatter keeps the whole document in memory, so its data structures
// are kept small: tokens refer to the source by offset, atoms are stored
// inline and comments live behind a pointer that is nil in the common case.

type fcomment struct {
	text     string
	block    bool
	newlines int // newlines before the comment
}

// fnode is a value: an atom (c == nil) or a collection.
type fnode struct {
	text string // atom text, or the name before a collection
	c    *fcoll
}

func (n *fnode) breaks() bool { return n.c != nil && n.c.breaks }

type fcoll struct {
	open      byte
	close     byte
	multiline bool // source had a line break between elements
	hug       bool // single nested collection printed as Name((...))
	expanded  bool // printed one element per line
	breaks    bool // printing produces line breaks
	closeN    int  // newlines before the closing delimiter
	items     []fitem
	afterOpen []fcomment // comments on the line of the opening delimiter
	tail      []fcomment // comments after the last item
}

type itemComments struct {
	leading  []fcomment
	trailing []fcomment
}

type fitem struct {
	key      fnode // map key, or the struct field name as an atom
	value    fnode
	cm       *itemComments
	newlines uint8 // newlines before the item's first token, capped at 2
	hasKey   bool  // key holds a map key
	isField  bool  // key holds a struct field name
}

func (it *fitem) leading() []fcomment {
	if it.cm == nil {
		return nil
	}
	return it.cm.leading
}

func (it *fitem) trailing() []fcomment {
	if it.cm == nil {
		return nil
	}
	return it.cm.trailing
}

func (it *fitem) comments() *itemComments {
	if it.cm == nil {
		it.cm = &itemComments{}
	}
	return it.cm
}

type fdoc struct {
	header   []fheader
	value    fnode
	valueN   int // newlines before the value
	trailing []fcomment
	after    []fcomment
}

type fheader struct {
	comment  *fcomment
	exts     []string
	newlines int
}

// ftok is the compact token the formatter keeps for the whole document.
// Literals are validated while lexing, so only the source span is needed.
type ftok struct {
	kind     Kind
	rawIdent bool
	newlines uint8 // capped at 2
	off      int32
	n        int32
}

type fparser struct {
	src   string
	toks  []ftok
	i     int
	stray []fcomment
	depth int
}

func capNewlines(n int) uint8 {
	if n > 2 {
		return 2
	}
	return uint8(n)
}

func newFParser(src []byte) (*fparser, error) {
	if len(src) > 1<<31-1 {
		return nil, Errorf(Position{Line: 1, Column: 1}, "input too large")
	}
	l := NewLexer(src)
	toks := make([]ftok, 0, len(src)/3+1)
	for {
		t, err := l.Next()
		if err != nil {
			return nil, err
		}
		switch t.Kind {
		case Int:
			if _, _, err := ParseInt(t); err != nil {
				return nil, Errorf(l.Resolve(t.Pos), "%s", err.Error())
			}
		case Float:
			if _, err := ParseFloat(t); err != nil {
				return nil, Errorf(l.Resolve(t.Pos), "%s", err.Error())
			}
		}
		toks = append(toks, ftok{
			kind:     t.Kind,
			rawIdent: t.RawIdent,
			newlines: capNewlines(t.Newlines),
			off:      int32(t.Pos.Offset),
			n:        int32(len(t.Raw)),
		})
		if t.Kind == EOF {
			break
		}
	}
	return &fparser{src: l.src, toks: toks}, nil
}

func (p *fparser) tok() *ftok { return &p.toks[p.i] }

func (p *fparser) raw(t *ftok) string { return p.src[t.off : t.off+t.n] }

// str returns an identifier's name without the r# prefix.
func (p *fparser) str(t *ftok) string {
	if t.rawIdent {
		return p.raw(t)[2:]
	}
	return p.raw(t)
}

// pos computes the position of a token. It is only needed for errors.
func (p *fparser) pos(t *ftok) Position {
	return ResolvePosition(p.src, int(t.off))
}

func (p *fparser) describe(t *ftok) string {
	return Token{Kind: t.kind, Raw: p.raw(t)}.Describe()
}

func (p *fparser) toComment(t *ftok) fcomment {
	text := p.raw(t)
	if t.kind == LineComment {
		text = strings.TrimRight(text, " \t\r")
	}
	return fcomment{text: text, block: t.kind == BlockComment, newlines: int(t.newlines)}
}

// comments consumes all comments at the current position.
func (p *fparser) comments() []fcomment {
	var cs []fcomment
	for p.tok().kind.IsComment() {
		cs = append(cs, p.toComment(p.tok()))
		p.i++
	}
	return cs
}

// sameLineComments consumes comments that start on the current line.
func (p *fparser) sameLineComments() []fcomment {
	var cs []fcomment
	for p.tok().kind.IsComment() && p.tok().newlines == 0 {
		cs = append(cs, p.toComment(p.tok()))
		p.i++
	}
	return cs
}

// skipComments moves comments in unusual positions, such as between a key
// and its colon, to the stray list. They are emitted before the enclosing
// item so no comment is ever lost.
func (p *fparser) skipComments() {
	for _, c := range p.comments() {
		c.newlines = 1
		p.stray = append(p.stray, c)
	}
}

func (p *fparser) errorf(format string, args ...any) error {
	return Errorf(p.pos(p.tok()), format, args...)
}

func (p *fparser) unexpected(want string) error {
	return p.errorf("expected %s, found %s", want, p.describe(p.tok()))
}

func (p *fparser) expect(k Kind) error {
	p.skipComments()
	if p.tok().kind != k {
		return p.unexpected(k.String())
	}
	p.i++
	return nil
}

// parseAttribute parses #![enable(a, b)] into h.exts.
func (p *fparser) parseAttribute(h *fheader) error {
	for _, k := range []Kind{Hash, Bang, LBracket} {
		if err := p.expect(k); err != nil {
			return err
		}
	}
	p.skipComments()
	if p.tok().kind != Ident || p.str(p.tok()) != "enable" {
		return p.unexpected("enable")
	}
	p.i++
	if err := p.expect(LParen); err != nil {
		return err
	}
	for {
		p.skipComments()
		if p.tok().kind == RParen {
			break
		}
		if p.tok().kind != Ident {
			return p.unexpected("extension name")
		}
		name := p.str(p.tok())
		if _, ok := ExtensionBit(name); !ok {
			return p.errorf("unknown extension %q", name)
		}
		h.exts = append(h.exts, name)
		p.i++
		p.skipComments()
		if p.tok().kind != Comma {
			break
		}
		p.i++
	}
	if err := p.expect(RParen); err != nil {
		return err
	}
	return p.expect(RBracket)
}

func (p *fparser) parseDoc() (*fdoc, error) {
	d := &fdoc{}
	for {
		for _, c := range p.comments() {
			d.header = append(d.header, fheader{comment: &c, newlines: c.newlines})
		}
		if p.tok().kind != Hash {
			break
		}
		h := fheader{newlines: int(p.tok().newlines)}
		if err := p.parseAttribute(&h); err != nil {
			return nil, err
		}
		d.header = append(d.header, h)
		for _, c := range p.stray {
			d.header = append(d.header, fheader{comment: &c, newlines: 1})
		}
		p.stray = nil
	}
	d.valueN = int(p.tok().newlines)
	v, err := p.parseNode()
	if err != nil {
		return nil, err
	}
	d.value = v
	for _, c := range p.stray {
		d.header = append(d.header, fheader{comment: &c, newlines: 1})
	}
	p.stray = nil
	d.trailing = p.sameLineComments()
	d.after = p.comments()
	if p.tok().kind != EOF {
		return nil, p.unexpected("end of input")
	}
	return d, nil
}

var reservedNoParens = map[string]bool{
	"true": true, "false": true, "None": true,
	"inf": true, "inff32": true, "inff64": true, "NaN": true, "NaNf32": true, "NaNf64": true,
}

func (p *fparser) parseNode() (fnode, error) {
	p.skipComments()
	t := p.tok()
	switch t.kind {
	case LParen, LBracket, LBrace:
		return p.parseColl("")
	case Ident:
		p.i++
		save := p.i
		strayLen := len(p.stray)
		p.skipComments()
		name := p.str(t)
		if p.tok().kind == LParen {
			if !t.rawIdent && reservedNoParens[name] {
				return fnode{}, p.unexpected("end of input")
			}
			n, err := p.parseColl(p.raw(t))
			if err != nil {
				return fnode{}, err
			}
			if !t.rawIdent && name == "Some" && (len(n.c.items) != 1 || n.c.items[0].isField) {
				return fnode{}, Errorf(p.pos(t), "Some takes exactly one value")
			}
			return n, nil
		}
		if !t.rawIdent && name == "Some" {
			return fnode{}, p.unexpected("'('")
		}
		p.i = save
		p.stray = p.stray[:strayLen]
		return fnode{text: p.raw(t)}, nil
	case Int, Float, String, ByteString, Char, Byte:
		p.i++
		return fnode{text: p.raw(t)}, nil
	}
	return fnode{}, p.unexpected("value")
}

func closeFor(open Kind) (Kind, byte, byte) {
	switch open {
	case LBracket:
		return RBracket, '[', ']'
	case LBrace:
		return RBrace, '{', '}'
	}
	return RParen, '(', ')'
}

func (p *fparser) parseColl(name string) (fnode, error) {
	c, err := p.parseCollBody()
	return fnode{text: name, c: c}, err
}

func (p *fparser) parseCollBody() (*fcoll, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > MaxDepth {
		return nil, p.errorf("exceeded maximum nesting depth of %d", MaxDepth)
	}
	closeKind, open, closeCh := closeFor(p.tok().kind)
	n := &fcoll{open: open, close: closeCh}
	p.i++
	mark := func(t *ftok) {
		if t.newlines > 0 {
			n.multiline = true
		}
	}
	n.afterOpen = p.sameLineComments()
	if len(n.afterOpen) > 0 {
		n.multiline = true
	}
	isStruct := open == '(' && p.firstIsField()
	for {
		lead := p.comments()
		if len(lead) > 0 {
			n.multiline = true
		}
		if p.tok().kind == closeKind {
			n.tail = lead
			n.closeN = int(p.tok().newlines)
			mark(p.tok())
			p.i++
			break
		}
		n.items = append(n.items, fitem{newlines: p.tok().newlines})
		it := &n.items[len(n.items)-1]
		if len(lead) > 0 {
			it.comments().leading = lead
		}
		mark(p.tok())
		switch {
		case open == '{':
			k, err := p.parseNode()
			if err != nil {
				return nil, err
			}
			it.key, it.hasKey = k, true
			p.skipComments()
			if p.tok().kind != Colon {
				return nil, p.unexpected("':'")
			}
			mark(p.tok())
			p.i++
			p.skipComments()
		case isStruct:
			if p.tok().kind != Ident {
				return nil, p.unexpected("field name")
			}
			it.key, it.isField = fnode{text: p.raw(p.tok())}, true
			p.i++
			p.skipComments()
			if p.tok().kind != Colon {
				return nil, p.unexpected("':'")
			}
			mark(p.tok())
			p.i++
			p.skipComments()
		}
		mark(p.tok())
		v, err := p.parseNode()
		if err != nil {
			return nil, err
		}
		it.value = v
		if len(p.stray) > 0 {
			cm := it.comments()
			cm.leading = append(cm.leading, p.stray...)
			p.stray = nil
			n.multiline = true
		}
		save := p.i
		before := p.comments()
		hadComma := p.tok().kind == Comma
		var trailing []fcomment
		if hadComma {
			mark(p.tok())
			p.i++
			trailing = before
			trailing = append(trailing, p.sameLineComments()...)
		} else {
			p.i = save
			trailing = p.sameLineComments()
		}
		if len(trailing) > 0 {
			it.comments().trailing = trailing
			n.multiline = true
		}
		if !hadComma {
			// Without a comma the collection must end here.
			save := p.i
			p.comments()
			if p.tok().kind != closeKind {
				return nil, p.errorf("expected ',' or '%c', found %s", closeCh, p.describe(p.tok()))
			}
			p.i = save
		}
	}

	// Layout decisions, bottom up.
	if open == '(' && len(n.items) == 1 && len(n.tail) == 0 && len(n.afterOpen) == 0 {
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
	return n, nil
}

// firstIsField reports whether a parenthesized collection starts with
// "ident:", which makes it a struct.
func (p *fparser) firstIsField() bool {
	j := p.i
	for j < len(p.toks) && p.toks[j].kind.IsComment() {
		j++
	}
	if j >= len(p.toks) || p.toks[j].kind != Ident {
		return false
	}
	for j++; j < len(p.toks) && p.toks[j].kind.IsComment(); j++ {
	}
	return j < len(p.toks) && p.toks[j].kind == Colon
}

type formatter struct {
	sb     bytes.Buffer
	indent string
	level  int
}

func (f *formatter) newline(blank bool) {
	if blank {
		f.sb.WriteByte('\n')
	}
	f.sb.WriteByte('\n')
	for range f.level {
		f.sb.WriteString(f.indent)
	}
}

func (f *formatter) doc(d *fdoc) {
	first := true
	for _, h := range d.header {
		if !first {
			f.newline(h.newlines > 1)
		}
		first = false
		if h.comment != nil {
			f.sb.WriteString(h.comment.text)
		} else {
			f.sb.WriteString("#![enable(")
			f.sb.WriteString(strings.Join(h.exts, ", "))
			f.sb.WriteString(")]")
		}
	}
	if !first {
		f.newline(d.valueN > 1)
	}
	f.node(&d.value)
	f.trailing(d.trailing)
	for _, c := range d.after {
		f.newline(c.newlines > 1)
		f.sb.WriteString(c.text)
	}
	f.sb.WriteByte('\n')
}

// trailing writes comments that follow an element on the same line. A line
// comment ends the line, so any comments after it go on their own lines.
func (f *formatter) trailing(cs []fcomment) {
	onLine := true
	for _, c := range cs {
		if onLine {
			f.sb.WriteByte(' ')
		} else {
			f.newline(false)
		}
		f.sb.WriteString(c.text)
		if !c.block {
			onLine = false
		}
	}
}

func (f *formatter) node(nd *fnode) {
	f.sb.WriteString(nd.text)
	n := nd.c
	if n == nil {
		return
	}
	f.sb.WriteByte(n.open)
	switch {
	case len(n.items) == 0 && len(n.tail) == 0 && len(n.afterOpen) == 0:
	case n.hug:
		f.node(&n.items[0].value)
	case !n.expanded:
		for i := range n.items {
			if i > 0 {
				f.sb.WriteString(", ")
			}
			f.itemBody(&n.items[i])
		}
	default:
		f.level++
		f.trailing(n.afterOpen)
		first := true
		for i := range n.items {
			it := &n.items[i]
			for _, c := range it.leading() {
				f.newline(!first && c.newlines > 1)
				f.sb.WriteString(c.text)
				first = false
			}
			f.newline(!first && it.newlines > 1)
			first = false
			f.itemBody(it)
			f.sb.WriteByte(',')
			f.trailing(it.trailing())
		}
		for _, c := range n.tail {
			f.newline(!first && c.newlines > 1)
			f.sb.WriteString(c.text)
			first = false
		}
		f.level--
		f.newline(false)
	}
	f.sb.WriteByte(n.close)
}

func (f *formatter) itemBody(it *fitem) {
	if it.hasKey || it.isField {
		f.node(&it.key)
		f.sb.WriteString(": ")
	}
	f.node(&it.value)
}
