// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"strings"
)

// Format reformats a RON document in a canonical style while preserving
// comments, literal spellings (number bases, raw strings, escapes) and the
// author's choice between single line and multi line collections:
//
//   - A collection whose contents span several lines in the input, or that
//     contains comments, is written with one element per line, indented by
//     four spaces, with trailing commas.
//   - A collection written on one line stays on one line, with ", " between
//     elements and ": " after keys.
//   - A single nested collection inside a tuple, such as Some((...)), hugs
//     its parent's parentheses.
//   - Single blank lines between elements are kept; longer runs are
//     collapsed.
//
// Format is idempotent. It returns a [*SyntaxError] if src is not valid RON.
func Format(src []byte) ([]byte, error) {
	return FormatIndent(src, "    ")
}

// FormatIndent is like [Format] but uses indent for each nesting level.
func FormatIndent(src []byte, indent string) ([]byte, error) {
	// Validate first so that semantic errors such as unknown extensions or
	// out of range literals are reported consistently with Parse.
	if _, err := ParseDocument(src); err != nil {
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
	f.doc(doc)
	return []byte(f.sb.String()), nil
}

type fcomment struct {
	text     string
	block    bool
	newlines int // newlines before the comment
}

type fnode struct {
	text      string // atom text, or the name before a collection
	open      byte   // 0 for atoms
	close     byte
	items     []*fitem
	afterOpen []fcomment // comments on the line of the opening delimiter
	tail      []fcomment // comments after the last item
	closeN    int        // newlines before the closing delimiter

	multiline bool // source had a line break between elements
	hug       bool // single nested collection printed as Name((...))
	expanded  bool // printed one element per line
	breaks    bool // printing produces line breaks
}

type fitem struct {
	leading  []fcomment
	newlines int    // newlines before the item's first token
	key      *fnode // map key
	field    string // struct field name, as written
	value    *fnode
	trailing []fcomment
}

type fdoc struct {
	header   []fheader
	value    *fnode
	valueN   int // newlines before the value
	trailing []fcomment
	after    []fcomment
}

type fheader struct {
	comment  *fcomment
	exts     []string
	newlines int
}

type fparser struct {
	toks  []token
	i     int
	stray []fcomment
	depth int
}

func newFParser(src []byte) (*fparser, error) {
	l := newLexer(src)
	var toks []token
	for {
		t, err := l.next()
		if err != nil {
			return nil, err
		}
		toks = append(toks, t)
		if t.kind == tokEOF {
			break
		}
	}
	return &fparser{toks: toks}, nil
}

func (p *fparser) tok() token { return p.toks[p.i] }

func isComment(t token) bool { return t.kind == tokLineComment || t.kind == tokBlockComment }

func toComment(t token) fcomment {
	text := t.raw
	if t.kind == tokLineComment {
		text = strings.TrimRight(text, " \t\r")
	}
	return fcomment{text: text, block: t.kind == tokBlockComment, newlines: t.newlines}
}

// comments consumes all comments at the current position.
func (p *fparser) comments() []fcomment {
	var cs []fcomment
	for isComment(p.tok()) {
		cs = append(cs, toComment(p.tok()))
		p.i++
	}
	return cs
}

// sameLineComments consumes comments that start on the current line.
func (p *fparser) sameLineComments() []fcomment {
	var cs []fcomment
	for isComment(p.tok()) && p.tok().newlines == 0 {
		cs = append(cs, toComment(p.tok()))
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
	l := &lexer{}
	return l.errorf(p.tok().pos, format, args...)
}

func (p *fparser) parseDoc() (*fdoc, error) {
	d := &fdoc{}
	for {
		for _, c := range p.comments() {
			d.header = append(d.header, fheader{comment: &c, newlines: c.newlines})
		}
		if p.tok().kind != tokHash {
			break
		}
		h := fheader{newlines: p.tok().newlines}
		// #![enable(a, b)]; validity was checked by ParseDocument.
		for p.tok().kind != tokRBracket {
			if p.tok().kind == tokIdent && p.tok().str != "enable" {
				h.exts = append(h.exts, p.tok().raw)
			}
			p.i++
			p.skipComments()
		}
		p.i++
		d.header = append(d.header, h)
	}
	d.valueN = p.tok().newlines
	v, err := p.parseNode()
	if err != nil {
		return nil, err
	}
	d.value = v
	if len(p.stray) > 0 {
		for _, c := range p.stray {
			d.header = append(d.header, fheader{comment: &c, newlines: 1})
		}
		p.stray = nil
	}
	d.trailing = p.sameLineComments()
	d.after = p.comments()
	if p.tok().kind != tokEOF {
		return nil, p.errorf("expected end of input")
	}
	return d, nil
}

func (p *fparser) parseNode() (*fnode, error) {
	p.skipComments()
	t := p.tok()
	switch t.kind {
	case tokLParen, tokLBracket, tokLBrace:
		return p.parseColl("")
	case tokIdent:
		p.i++
		save := p.i
		strayLen := len(p.stray)
		p.skipComments()
		if p.tok().kind == tokLParen {
			return p.parseColl(t.raw)
		}
		p.i = save
		p.stray = p.stray[:strayLen]
		return &fnode{text: t.raw}, nil
	case tokInt, tokFloat, tokString, tokByteString, tokChar, tokByte:
		p.i++
		return &fnode{text: t.raw}, nil
	}
	return nil, p.errorf("expected value")
}

func closeFor(open tokenKind) (tokenKind, byte, byte) {
	switch open {
	case tokLBracket:
		return tokRBracket, '[', ']'
	case tokLBrace:
		return tokRBrace, '{', '}'
	}
	return tokRParen, '(', ')'
}

func (p *fparser) parseColl(name string) (*fnode, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxDepth {
		return nil, p.errorf("exceeded maximum nesting depth of %d", maxDepth)
	}
	closeKind, open, closeCh := closeFor(p.tok().kind)
	n := &fnode{text: name, open: open, close: closeCh}
	p.i++
	mark := func(t token) {
		if t.newlines > 0 {
			n.multiline = true
		}
	}
	n.afterOpen = p.sameLineComments()
	if len(n.afterOpen) > 0 {
		n.multiline = true
	}
	for {
		lead := p.comments()
		if len(lead) > 0 {
			n.multiline = true
		}
		if p.tok().kind == closeKind {
			n.tail = lead
			n.closeN = p.tok().newlines
			mark(p.tok())
			p.i++
			break
		}
		it := &fitem{leading: lead, newlines: p.tok().newlines}
		mark(p.tok())
		switch {
		case open == '{':
			k, err := p.parseNode()
			if err != nil {
				return nil, err
			}
			it.key = k
			p.skipComments()
			if p.tok().kind != tokColon {
				return nil, p.errorf("expected ':'")
			}
			mark(p.tok())
			p.i++
			p.skipComments()
		case open == '(' && p.tok().kind == tokIdent && p.nextNonComment().kind == tokColon:
			it.field = p.tok().raw
			p.i++
			p.skipComments()
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
			it.leading = append(it.leading, p.stray...)
			p.stray = nil
			n.multiline = true
		}
		save := p.i
		before := p.comments()
		hadComma := p.tok().kind == tokComma
		if hadComma {
			mark(p.tok())
			p.i++
			it.trailing = append(before, p.sameLineComments()...)
		} else {
			p.i = save
			it.trailing = p.sameLineComments()
		}
		if len(it.trailing) > 0 {
			n.multiline = true
		}
		n.items = append(n.items, it)
		if !hadComma {
			// Without a comma the collection must end here.
			save := p.i
			p.comments()
			if p.tok().kind != closeKind {
				return nil, p.errorf("expected ',' or '%c'", closeCh)
			}
			p.i = save
		}
	}

	// Layout decisions, bottom up.
	if open == '(' && len(n.items) == 1 && len(n.tail) == 0 && len(n.afterOpen) == 0 {
		it := n.items[0]
		if it.field == "" && len(it.leading) == 0 && len(it.trailing) == 0 &&
			it.value.open != 0 && it.newlines == 0 && n.closeN == 0 {
			n.hug = true
		}
	}
	if !n.hug {
		n.expanded = n.multiline || len(n.tail) > 0 || len(n.afterOpen) > 0
		for _, it := range n.items {
			if len(it.leading) > 0 || len(it.trailing) > 0 || it.value.breaks || (it.key != nil && it.key.breaks) {
				n.expanded = true
			}
		}
		n.breaks = n.expanded
	} else {
		n.breaks = n.items[0].value.breaks
	}
	return n, nil
}

func (p *fparser) nextNonComment() token {
	for j := p.i + 1; j < len(p.toks); j++ {
		if !isComment(p.toks[j]) {
			return p.toks[j]
		}
	}
	return p.toks[len(p.toks)-1]
}

type formatter struct {
	sb     strings.Builder
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
	f.node(d.value)
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

func (f *formatter) node(n *fnode) {
	f.sb.WriteString(n.text)
	if n.open == 0 {
		return
	}
	f.sb.WriteByte(n.open)
	switch {
	case len(n.items) == 0 && len(n.tail) == 0 && len(n.afterOpen) == 0:
	case n.hug:
		f.node(n.items[0].value)
	case !n.expanded:
		for i, it := range n.items {
			if i > 0 {
				f.sb.WriteString(", ")
			}
			f.itemBody(it)
		}
	default:
		f.level++
		f.trailing(n.afterOpen)
		first := true
		for _, it := range n.items {
			for _, c := range it.leading {
				f.newline(!first && c.newlines > 1)
				f.sb.WriteString(c.text)
				first = false
			}
			f.newline(!first && it.newlines > 1)
			first = false
			f.itemBody(it)
			f.sb.WriteByte(',')
			f.trailing(it.trailing)
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
	switch {
	case it.key != nil:
		f.node(it.key)
		f.sb.WriteString(": ")
	case it.field != "":
		f.sb.WriteString(it.field)
		f.sb.WriteString(": ")
	}
	f.node(it.value)
}
