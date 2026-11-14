// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package syntax

import (
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
	toks  []ftok
	i     int
	stray []fcomment
	depth int
}

// ftok is the compact token the formatter keeps for the whole document.
// Literals are validated while lexing, so only the source text is needed.
type ftok struct {
	Kind     Kind
	RawIdent bool
	Newlines int
	Pos      Position
	Raw      string
}

// Str returns an identifier's name without the r# prefix.
func (t ftok) Str() string {
	if t.RawIdent {
		return t.Raw[2:]
	}
	return t.Raw
}

func (t ftok) Describe() string {
	return Token{Kind: t.Kind, Raw: t.Raw}.Describe()
}

func newFParser(src []byte) (*fparser, error) {
	l := NewLexer(src)
	toks := make([]ftok, 0, len(src)/8+1)
	for {
		t, err := l.Next()
		if err != nil {
			return nil, err
		}
		switch t.Kind {
		case Int:
			if _, _, err := ParseInt(t); err != nil {
				return nil, Errorf(t.Pos, "%s", err.Error())
			}
		case Float:
			if _, err := ParseFloat(t); err != nil {
				return nil, Errorf(t.Pos, "%s", err.Error())
			}
		}
		toks = append(toks, ftok{Kind: t.Kind, RawIdent: t.RawIdent, Newlines: t.Newlines, Pos: t.Pos, Raw: t.Raw})
		if t.Kind == EOF {
			break
		}
	}
	return &fparser{toks: toks}, nil
}

func (p *fparser) tok() *ftok { return &p.toks[p.i] }

func toComment(t *ftok) fcomment {
	text := t.Raw
	if t.Kind == LineComment {
		text = strings.TrimRight(text, " \t\r")
	}
	return fcomment{text: text, block: t.Kind == BlockComment, newlines: t.Newlines}
}

// comments consumes all comments at the current position.
func (p *fparser) comments() []fcomment {
	var cs []fcomment
	for p.tok().Kind.IsComment() {
		cs = append(cs, toComment(p.tok()))
		p.i++
	}
	return cs
}

// sameLineComments consumes comments that start on the current line.
func (p *fparser) sameLineComments() []fcomment {
	var cs []fcomment
	for p.tok().Kind.IsComment() && p.tok().Newlines == 0 {
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
	return Errorf(p.tok().Pos, format, args...)
}

func (p *fparser) unexpected(want string) error {
	return p.errorf("expected %s, found %s", want, p.tok().Describe())
}

func (p *fparser) expect(k Kind) error {
	p.skipComments()
	if p.tok().Kind != k {
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
	if p.tok().Kind != Ident || p.tok().Str() != "enable" {
		return p.unexpected("enable")
	}
	p.i++
	if err := p.expect(LParen); err != nil {
		return err
	}
	for {
		p.skipComments()
		if p.tok().Kind == RParen {
			break
		}
		if p.tok().Kind != Ident {
			return p.unexpected("extension name")
		}
		if _, ok := ExtensionBit(p.tok().Str()); !ok {
			return p.errorf("unknown extension %q", p.tok().Str())
		}
		h.exts = append(h.exts, p.tok().Str())
		p.i++
		p.skipComments()
		if p.tok().Kind != Comma {
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
		if p.tok().Kind != Hash {
			break
		}
		h := fheader{newlines: p.tok().Newlines}
		if err := p.parseAttribute(&h); err != nil {
			return nil, err
		}
		d.header = append(d.header, h)
		for _, c := range p.stray {
			d.header = append(d.header, fheader{comment: &c, newlines: 1})
		}
		p.stray = nil
	}
	d.valueN = p.tok().Newlines
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
	if p.tok().Kind != EOF {
		return nil, p.unexpected("end of input")
	}
	return d, nil
}

var reservedNoParens = map[string]bool{"true": true, "false": true, "inf": true, "NaN": true, "None": true}

func (p *fparser) parseNode() (*fnode, error) {
	p.skipComments()
	t := p.tok()
	switch t.Kind {
	case LParen, LBracket, LBrace:
		return p.parseColl("")
	case Ident:
		p.i++
		save := p.i
		strayLen := len(p.stray)
		p.skipComments()
		if p.tok().Kind == LParen {
			if !t.RawIdent && reservedNoParens[t.Str()] {
				return nil, p.unexpected("end of input")
			}
			n, err := p.parseColl(t.Raw)
			if err != nil {
				return nil, err
			}
			if !t.RawIdent && t.Str() == "Some" && (len(n.items) != 1 || n.items[0].field != "") {
				return nil, Errorf(t.Pos, "Some takes exactly one value")
			}
			return n, nil
		}
		if !t.RawIdent && t.Str() == "Some" {
			return nil, p.unexpected("'('")
		}
		p.i = save
		p.stray = p.stray[:strayLen]
		return &fnode{text: t.Raw}, nil
	case Int, Float, String, ByteString, Char, Byte:
		p.i++
		return &fnode{text: t.Raw}, nil
	}
	return nil, p.unexpected("value")
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

func (p *fparser) parseColl(name string) (*fnode, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > MaxDepth {
		return nil, p.errorf("exceeded maximum nesting depth of %d", MaxDepth)
	}
	closeKind, open, closeCh := closeFor(p.tok().Kind)
	n := &fnode{text: name, open: open, close: closeCh}
	p.i++
	mark := func(t *ftok) {
		if t.Newlines > 0 {
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
		if p.tok().Kind == closeKind {
			n.tail = lead
			n.closeN = p.tok().Newlines
			mark(p.tok())
			p.i++
			break
		}
		it := &fitem{leading: lead, newlines: p.tok().Newlines}
		mark(p.tok())
		switch {
		case open == '{':
			k, err := p.parseNode()
			if err != nil {
				return nil, err
			}
			it.key = k
			p.skipComments()
			if p.tok().Kind != Colon {
				return nil, p.unexpected("':'")
			}
			mark(p.tok())
			p.i++
			p.skipComments()
		case isStruct:
			if p.tok().Kind != Ident {
				return nil, p.unexpected("field name")
			}
			it.field = p.tok().Raw
			p.i++
			p.skipComments()
			if p.tok().Kind != Colon {
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
			it.leading = append(it.leading, p.stray...)
			p.stray = nil
			n.multiline = true
		}
		save := p.i
		before := p.comments()
		hadComma := p.tok().Kind == Comma
		if hadComma {
			mark(p.tok())
			p.i++
			it.trailing = before
			it.trailing = append(it.trailing, p.sameLineComments()...)
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
			if p.tok().Kind != closeKind {
				return nil, p.errorf("expected ',' or '%c', found %s", closeCh, p.tok().Describe())
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

// firstIsField reports whether a parenthesized collection starts with
// "ident:", which makes it a struct.
func (p *fparser) firstIsField() bool {
	j := p.i
	for j < len(p.toks) && p.toks[j].Kind.IsComment() {
		j++
	}
	if j >= len(p.toks) || p.toks[j].Kind != Ident {
		return false
	}
	for j++; j < len(p.toks) && p.toks[j].Kind.IsComment(); j++ {
	}
	return j < len(p.toks) && p.toks[j].Kind == Colon
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
